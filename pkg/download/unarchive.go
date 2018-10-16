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

package download

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"

	"github.com/golang/glog"
	"github.com/pkg/errors"
)

// Unarchiver describes an archive extractor.
type Unarchiver func(dst string, r io.ReaderAt, size int64) error

// NewZIPUnarchiver returns a .zip unarchiver.
func NewZIPUnarchiver() Unarchiver { return extractZIP }

// NewTARGZUnarchiver returns a .tar.gz unarchiver.
func NewTARGZUnarchiver() Unarchiver {
	return func(dst string, r io.ReaderAt, size int64) error {
		return extractTARGZ(dst, io.NewSectionReader(r, 0, size))
	}
}

// extractZIP extracts a zip file into the target directory.
func extractZIP(targetDir string, read io.ReaderAt, size int64) error {
	glog.V(4).Infof("Extracting download zip to %q", targetDir)
	zipReader, err := zip.NewReader(read, size)
	if err != nil {
		return err
	}

	for _, f := range zipReader.File {
		path := filepath.Join(targetDir, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
			continue
		}

		src, err := f.Open()
		if err != nil {
			return errors.Wrap(err, "could not open inflating zip file")
		}

		dst, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			return errors.Wrap(err, "can't create file in zip destination dir")
		}

		if _, err := io.Copy(dst, src); err != nil {
			return errors.Wrap(err, "can't copy content to zip destination file")
		}

		// Cleanup the open fd. Don't use defer in case of many files.
		// Don't be blocking
		src.Close()
		dst.Close()
	}

	return nil
}

// extractTARGZ extracts a gzipped tar file into the target directory.
func extractTARGZ(targetDir string, in io.Reader) error {
	glog.V(4).Infof("tar: extracting to %q", targetDir)

	gzr, err := gzip.NewReader(in)
	if err != nil {
		return errors.Wrap(err, "failed to create gzip reader")
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Wrap(err, "tar extraction error")
		}
		glog.V(4).Infof("tar: processing %q (type=%d, mode=%s)", hdr.Name, hdr.Typeflag, os.FileMode(hdr.Mode))
		// see https://golang.org/cl/78355 for handling pax_global_header
		if hdr.Name == "pax_global_header" {
			glog.V(4).Infof("tar: skipping pax_global_header file")
			continue
		}

		path := filepath.Join(targetDir, filepath.FromSlash(hdr.Name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, os.FileMode(hdr.Mode)); err != nil {
				return errors.Wrap(err, "failed to create directory from tar")
			}
		case tar.TypeReg:
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, os.FileMode(hdr.Mode))
			if err != nil {
				return errors.Wrapf(err, "failed to create file %q", path)
			}
			if _, err := io.Copy(f, tr); err != nil {
				return errors.Wrapf(err, "failed to copy %q from tar into file", hdr.Name)
			}
		default:
			return errors.Errorf("unable to handle file type %d for %q in tar", hdr.Typeflag, hdr.Name)
		}
		glog.V(4).Infof("tar: processed %q", hdr.Name)
	}
	glog.V(4).Infof("tar extraction to %s complete", targetDir)
	return nil
}
