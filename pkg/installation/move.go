// Copyright © 2018 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package installation

import (
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/GoogleContainerTools/krew/pkg/index"
	"github.com/GoogleContainerTools/krew/pkg/pathutil"

	"github.com/golang/glog"
	"github.com/pkg/errors"
)

type move struct {
	from, to string
}

func findMoveTargets(fromDir, toDir string, fo index.FileOperation) ([]move, error) {
	if fo.To != filepath.Clean(fo.To) {
		return nil, errors.Errorf("the provided path is not clean, %q should be %q", fo.To, filepath.Clean(fo.To))
	}
	fromDir, err := filepath.Abs(fromDir)
	if err != nil {
		return nil, errors.Wrap(err, "could not get the relative path for the move src")
	}

	glog.V(4).Infof("Trying to move single file directly from=%q to=%q with file operation=%#v", fromDir, toDir, fo)
	if m, ok, err := getDirectMove(fromDir, toDir, fo); err != nil {
		return nil, errors.Wrap(err, "failed to detect single move operation")
	} else if ok {
		glog.V(3).Infof("Detected single move from file operation=%#v", fo)
		return []move{m}, nil
	}

	glog.V(4).Infoln("Wasn't a single file, proceeding with Glob move")
	newDir, err := filepath.Abs(filepath.Join(filepath.FromSlash(toDir), filepath.FromSlash(fo.To)))
	if err != nil {
		return nil, errors.Wrap(err, "could not get the relative path for the move dst")
	}

	gl, err := filepath.Glob(filepath.Join(filepath.FromSlash(fromDir), filepath.FromSlash(fo.From)))
	if err != nil {
		return nil, errors.Wrap(err, "could not get files using a glob string")
	}
	if len(gl) == 0 {
		return nil, errors.Errorf("no files in the plugin archive matched the glob pattern=%s", fo.From)
	}

	var moves []move
	for _, v := range gl {
		newPath := filepath.Join(newDir, filepath.Base(filepath.FromSlash(v)))
		// Check secure path
		m := move{from: v, to: newPath}
		if !isMoveAllowed(fromDir, toDir, m) {
			return nil, errors.Errorf("can't move, move target %v is not a subpath from=%q, to=%q", m, fromDir, toDir)
		}
		moves = append(moves, m)
	}
	return moves, nil
}

func getDirectMove(fromDir, toDir string, fo index.FileOperation) (move, bool, error) {
	var m move
	fromDir, err := filepath.Abs(fromDir)
	if err != nil {
		return m, false, errors.Wrap(err, "could not get the relative path for the move src")
	}

	toDir, err = filepath.Abs(toDir)
	if err != nil {
		return m, false, errors.Wrap(err, "could not get the relative path for the move src")
	}

	// Check is direct file (not a Glob)
	fromFilePath := filepath.Clean(filepath.Join(fromDir, fo.From))
	_, err = os.Stat(fromFilePath)
	if err != nil {
		return m, false, nil
	}

	// If target is empty use old file name.
	if filepath.Clean(fo.To) == "." {
		fo.To = filepath.Base(fromFilePath)
	}

	// Build new file name
	toFilePath, err := filepath.Abs(filepath.Join(filepath.FromSlash(toDir), filepath.FromSlash(fo.To)))
	if err != nil {
		return m, false, errors.Wrap(err, "could not get the relative path for the move dst")
	}

	// Check sane path
	m = move{from: fromFilePath, to: toFilePath}
	if !isMoveAllowed(fromDir, toDir, m) {
		return move{}, false, errors.Errorf("can't move, move target %v is out of bounds from=%q, to=%q", m, fromDir, toDir)
	}

	return m, true, nil
}

func isMoveAllowed(fromBase, toBase string, m move) bool {
	_, okFrom := pathutil.IsSubPath(fromBase, m.from)
	_, okTo := pathutil.IsSubPath(toBase, m.to)
	return okFrom && okTo
}

func moveFiles(fromDir, toDir string, fo index.FileOperation) error {
	glog.V(4).Infof("Finding move targets from %q to %q with file operation=%#v", fromDir, toDir, fo)
	moves, err := findMoveTargets(fromDir, toDir, fo)
	if err != nil {
		return errors.Wrap(err, "could not find move targets")
	}

	for _, m := range moves {
		glog.V(2).Infof("Move file from %q to %q", m.from, m.to)
		if err := os.MkdirAll(filepath.Dir(m.to), 0755); err != nil {
			return errors.Wrapf(err, "failed to create move path %q", filepath.Dir(m.to))
		}

		if err = os.Rename(m.from, m.to); err != nil {
			return errors.Wrapf(err, "could not rename file from %q to %q", m.from, m.to)
		}
	}
	glog.V(4).Infoln("Move operations are complete")
	return nil
}

func moveAllFiles(fromDir, toDir string, fos []index.FileOperation) error {
	for _, fo := range fos {
		if err := moveFiles(fromDir, toDir, fo); err != nil {
			return errors.Wrap(err, "failed moving files")
		}
	}
	return nil
}

// moveOrCopyDir will try to rename a dir or file. If rename is not supported a
// manual copy will be performed. Existing files at "to" will be deleted
func moveOrCopyDir(from, to string) error {
	// Try atomic rename (does not work cross partition).
	fi, err := os.Stat(to)
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "error checking move target dir %q", to)
	}
	if fi != nil && fi.IsDir() {
		glog.V(4).Infof("There's already a directory at move target %q. deleting.", to)
		if err := os.RemoveAll(to); err != nil {
			return errors.Wrapf(err, "error cleaning up dir %q", to)
		}
		glog.V(4).Infof("Move target directory %q cleaned up", to)
	}

	err = os.Rename(from, to)
	// Fallback for invalid cross-device link (errno:18).
	if le, ok := err.(*os.LinkError); err != nil && ok {
		if errno, ok := le.Err.(syscall.Errno); ok && errno == 18 {
			glog.V(4).Infof("Cross-device link error (ERRNO=18), fallback to manual copy")
			return copyDir(from, to)
		}
	}
	return err
}

func copyDir(from string, to string) (err error) {
	return filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		newPath, _ := pathutil.ReplaceBase(path, from, to)
		if info.IsDir() {
			glog.V(4).Infof("Creating new dir %q", newPath)
			err = os.MkdirAll(newPath, info.Mode())
		} else {
			glog.V(4).Infof("Copying file %q", newPath)
			err = copyFile(path, newPath, info.Mode())
		}
		return err
	})
}

func copyFile(source string, dst string, mode os.FileMode) (err error) {
	sf, err := os.Open(source)
	if err != nil {
		return err
	}
	defer sf.Close()

	df, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer df.Close()

	_, err = io.Copy(df, sf)
	if err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}
