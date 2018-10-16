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
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/GoogleContainerTools/krew/pkg/download"
	"github.com/GoogleContainerTools/krew/pkg/environment"
	"github.com/GoogleContainerTools/krew/pkg/index"
	"github.com/GoogleContainerTools/krew/pkg/pathutil"
	"github.com/pkg/errors"

	"github.com/golang/glog"
)

// Plugin Lifecycle Errors
var (
	ErrIsAlreadyInstalled = errors.New("can't install, the newest version is already installed")
	ErrIsNotInstalled     = errors.New("plugin is not installed")
	ErrIsAlreadyUpgraded  = errors.New("can't upgrade, the newest version is already installed")
)

const (
	headVersion    = "HEAD"
	headOldVersion = "HEAD-OLD"
	krewPluginName = "krew"
)

// Install will download and install a plugin. The operation tries
// to not get the plugin dir in a bad state if it fails during the process.
func Install(p environment.Paths, plugin index.Plugin, forceHEAD bool, forceDownloadFile string) error {
	name := plugin.Name
	glog.V(2).Infof("Looking for installed versions of %q", name)
	_, ok, err := findInstalledPluginVersion(p.InstallPath(), p.BinPath(), name)
	if err != nil {
		return err
	} else if ok {
		return ErrIsAlreadyInstalled
	}
	glog.V(2).Infof("Plugin %q not installed", name)

	userOS, userArch := osArch()
	glog.V(2).Infof("Finding matching (%s/%s) among %d platforms", userOS, userArch, len(plugin.Spec.Platforms))
	platform, ok, err := matchPlatformToSystemEnvs(plugin.Spec.Platforms, userOS, userArch)
	if err != nil {
		return errors.Wrap(err, "failed to find matching platform")
	} else if !ok {
		return errors.Errorf("plugin does not support platform %s/%s", userOS, userArch)
	}
	glog.V(2).Infof("Found a matching platform for plugin %q", name)

	version, downloadURL, err := choosePluginVersion(platform, forceHEAD)
	if err != nil {
		return errors.Wrap(err, "could not choose a version to download")
	}
	if forceDownloadFile != "" {
		glog.V(1).Infof("Overriding download url with local file=%s", forceDownloadFile)
		downloadURL = forceDownloadFile
	}
	glog.V(2).Infof("Chosen version=%s (url=%s) for plugin %q", version, downloadURL, name)

	verifier := initVerifier(version == headVersion, platform.Sha256)
	unarchiver, err := initUnarchiver(downloadURL)
	if err != nil {
		return errors.Wrap(err, "cannot initialize unarchiver")
	}
	fetcher := initFetcher(downloadURL, forceDownloadFile)

	body, err := fetcher.Get()
	if err != nil {
		return errors.Wrap(err, "download failure")
	}
	defer body.Close()
	glog.V(3).Infof("Reading downloaded file into memory")
	data, err := ioutil.ReadAll(io.TeeReader(body, verifier))
	if err != nil {
		return errors.Wrap(err, "could not read download content")
	}
	glog.V(2).Infof("Read %d bytes of download data into memory", len(data))
	if err := verifier.Verify(); err != nil {
		return errors.Wrap(err, "download could not be verified")
	}
	extractDir := filepath.Join(p.DownloadPath(), name)
	glog.V(2).Infof("Unarchiving downloaded file. dst=%s", extractDir)
	if err := unarchiver(extractDir, bytes.NewReader(data), int64(len(data))); err != nil {
		return errors.Wrap(err, "extract failure")
	}
	glog.V(2).Info("File unarchived.")

	stagingDir, err := ioutil.TempDir("", "krew-temp-move")
	glog.V(4).Infof("Creating staging directory path=%s", stagingDir)
	if err != nil {
		return errors.Wrap(err, "failed to find a temporary directory")
	}
	defer func() {
		glog.V(4).Infof("Cleaning up staging directory=%s", stagingDir)
		os.RemoveAll(stagingDir)
	}()
	glog.V(2).Infof("Starting file copy operations (%d)", len(platform.Files))
	if err = moveAllFiles(extractDir, stagingDir, platform.Files); err != nil {
		return errors.Wrap(err, "failed to move files")
	}
	glog.V(2).Infof("File copy operations complete.")

	installDir := p.PluginVersionInstallPath(name, version)
	glog.V(2).Infof("Ensuring plugin installation direcotry=%s", installDir)
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return errors.Wrap(err, "could not create installation directory")
	}
	glog.V(2).Info("Moving staging directory to installation directory.")
	if err = moveOrCopyDir(stagingDir, installDir); err != nil {
		defer func() {
			glog.V(4).Infof("Cleaning up installation directory=%s", installDir)
			os.Remove(installDir)
		}()
		return errors.Wrapf(err, "could not rename from=%q to=%q", stagingDir, installDir)
	}

	executablePath := filepath.Join(installDir, filepath.FromSlash(platform.Bin))
	glog.V(4).Infof("Checking if it is safe to create symlink for executable=%s", executablePath)
	if err := evaluateBinPath(installDir, executablePath); err != nil {
		return errors.Wrap(err, "symlink unsafe")
	}

	linkPath := filepath.Join(p.BinPath(), pluginNameToBin(name, isWindows())) // TODO(ahmetb) this should be offered by environment.Paths.
	glog.V(2).Infof("Creating symlink for plugin %q at=%s", name, linkPath)
	if err := createOrUpdateLink(executablePath, linkPath); err != nil {
		return errors.Wrap(err, "cannot link plugin")
	}
	glog.V(1).Infof("Symbolic link created at=%s", linkPath)
	return nil
}

// evaluateBinPath determines if the given installDir+executablePath path is
// still within installDir. This prevents creating executable references outside
// the installation directory.
func evaluateBinPath(installDir string, executablePath string) error {
	installDirAbs, err := filepath.Abs(installDir)
	if err != nil {
		return err
	}
	executablePathAbs, err := filepath.Abs(executablePath)
	if err != nil {
		return err
	}
	if _, ok := pathutil.IsSubPath(installDir, executablePath); !ok {
		return errors.Wrapf(err, "executable path (%s) is not within installation directory (%d)", executablePath, installDir)
	}
	return nil
}

// Remove will remove a plugin.
func Remove(p environment.Paths, name string) error {
	if name == krewPluginName {
		return errors.New("removing krew is not allowed through krew, see docs for help")
	}
	glog.V(3).Infof("Finding installed version to delete")
	version, installed, err := findInstalledPluginVersion(p.InstallPath(), p.BinPath(), name)
	if err != nil {
		return errors.Wrap(err, "can't remove plugin")
	}
	if !installed {
		return ErrIsNotInstalled
	}
	glog.V(1).Infof("Deleting plugin version %s", version)
	glog.V(3).Infof("Deleting path %q", p.PluginInstallPath(name))

	symlinkPath := filepath.Join(p.BinPath(), pluginNameToBin(name, isWindows()))
	if err := removeLink(symlinkPath); err != nil {
		return errors.Wrap(err, "could not uninstall symlink of plugin")
	}
	return os.RemoveAll(p.PluginInstallPath(name))
}

func initVerifier(isHEAD bool, checksum string) download.Verifier {
	if isHEAD {
		return download.NewTrueVerifier()
	}
	return download.NewSHA256Verifier(checksum)
}

func initUnarchiver(filename string) (download.Unarchiver, error) {
	if strings.HasSuffix(filename, ".zip") {
		return download.NewZIPUnarchiver(), nil
	} else if strings.HasSuffix(filename, ".tar.gz") {
		return download.NewTARGZUnarchiver(), nil
	}
	return nil, errors.Errorf("cannot infer a supported archive type from filename in the url (%q)", filename)
}

func initFetcher(url, fileOverride string) download.Fetcher {
	if fileOverride != "" {
		return download.NewFileFetcher(fileOverride)
	}
	return download.NewHTTPFetcher(url)
}

// createOrUpdateLink ensures a symlink to src at dst.
func createOrUpdateLink(src string, dst string) error {
	if err := removeLink(dst); err != nil {
		return errors.Wrap(err, "failed to remove old symlink")
	}
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return errors.Wrapf(err, "executable to be symlinked cannot be found at=%s", src)
	}
	err := os.Symlink(src, dst)
	return errors.Wrap(err, "failed to create a symlink")

}

// removeLink removes a symlink reference if exists.
func removeLink(path string) error {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		glog.V(3).Infof("No file found at %q", path)
		return nil
	} else if err != nil {
		return errors.Wrapf(err, "failed to read the symlink in %q", path)
	}

	if fi.Mode()&os.ModeSymlink == 0 {
		return errors.Errorf("file %q is not a symlink (mode=%s)", path, fi.Mode())
	}
	if err := os.Remove(path); err != nil {
		return errors.Wrapf(err, "failed to remove the symlink in %q", path)
	}
	glog.V(3).Infof("Removed symlink from %q", path)
	return nil
}

func isWindows() bool {
	goos := runtime.GOOS
	if env := os.Getenv("KREW_OS"); env != "" {
		goos = env
	}
	return goos == "windows"
}

// pluginNameToBin creates the name of the symlink file for the plugin name.
// It converts dashes to underscores.
func pluginNameToBin(name string, isWindows bool) string {
	name = strings.Replace(name, "-", "_", -1)
	name = "kubectl-" + name
	if isWindows {
		name = name + ".exe"
	}
	return name
}
