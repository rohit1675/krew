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
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/golang/glog"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/GoogleContainerTools/krew/pkg/index"
	"github.com/GoogleContainerTools/krew/pkg/pathutil"
)

// osArch returns the OS/arch combination to be used on the current system. It
// can be overridden by setting KREW_OS and/or KREW_ARCH environment variables.
func osArch() (string, string) {
	goos, goarch := runtime.GOOS, runtime.GOARCH
	envOS, envArch := os.Getenv("KREW_OS"), os.Getenv("KREW_ARCH")
	if envOS != "" {
		goos = envOS
	}
	if envArch != "" {
		goarch = envArch
	}
	return goos, goarch
}

func matchPlatformToSystemEnvs(platforms []index.Platform, os, arch string) (index.Platform, bool, error) {
	envLabels := labels.Set{
		"os":   os,
		"arch": arch,
	}
	for i, platform := range platforms {
		sel, err := metav1.LabelSelectorAsSelector(platform.Selector)
		if err != nil {
			return index.Platform{}, false, errors.Wrap(err, "failed to compile label selector")
		}
		if sel.Matches(envLabels) {
			glog.V(4).Infof("Found matching platform with index (%d)", i)
			return platform, true, nil
		}
	}
	return index.Platform{}, false, nil
}

func findInstalledPluginVersion(installPath, binDir, pluginName string) (name string, installed bool, err error) {
	if !index.IsSafePluginName(pluginName) {
		return "", false, errors.Errorf("the plugin name %q is not allowed", pluginName)
	}
	glog.V(3).Infof("Searching for installed versions of %s in %q", pluginName, binDir)
	link, err := os.Readlink(filepath.Join(binDir, pluginNameToBin(pluginName, isWindows())))
	if os.IsNotExist(err) {
		return "", false, nil
	} else if err != nil {
		return "", false, errors.Wrap(err, "could not read plugin link")
	}

	if !filepath.IsAbs(link) {
		if link, err = filepath.Abs(filepath.Join(binDir, link)); err != nil {
			return "", true, errors.Wrapf(err, "failed to get the absolute path for the link of %q", link)
		}
	}

	name, err = pluginVersionFromPath(installPath, link)
	if err != nil {
		return "", true, errors.Wrap(err, "cloud not parse plugin version")
	}
	return name, true, nil
}

func pluginVersionFromPath(installPath, pluginPath string) (string, error) {
	// plugin path: {install_path}/{plugin_name}/{version}/...
	elems, ok := pathutil.IsSubPath(installPath, pluginPath)
	if !ok || len(elems) < 2 {
		return "", errors.Errorf("failed to get the version from execution path=%q, with install path=%q", pluginPath, installPath)
	}
	return elems[1], nil
}

func choosePluginVersion(p index.Platform, forceHEAD bool) (version, uri string, err error) {
	if (forceHEAD && p.Head != "") || (p.Head != "" && p.Sha256 == "" && p.URI == "") {
		return headVersion, p.Head, nil
	}
	if forceHEAD && p.Head == "" {
		return "", "", errors.New("can't force HEAD, plugin manifest does not have a \"head\"")
	}
	return strings.ToLower(p.Sha256), p.URI, nil // TODO(ahmetb) lowering sha here is not useful. just do case-insensitive match where the comparison happens.
}

// ListInstalledPlugins returns a list of all name:version for all plugins.
func ListInstalledPlugins(installDir, binDir string) (map[string]string, error) {
	installed := make(map[string]string)
	plugins, err := ioutil.ReadDir(installDir)
	if err != nil {
		return installed, errors.Wrap(err, "failed to read install dir")
	}
	glog.V(4).Infof("Read installation directory: %s (%d items)", installDir, len(plugins))
	for _, plugin := range plugins {
		if !plugin.IsDir() {
			glog.V(4).Infof("Skip non-directory item: %s", plugin.Name())
			continue
		}
		version, ok, err := findInstalledPluginVersion(installDir, binDir, plugin.Name())
		if err != nil {
			return installed, errors.Wrap(err, "failed to get plugin version")
		}
		if ok {
			installed[plugin.Name()] = version
			glog.V(4).Infof("Found %q, with version %s", plugin.Name(), version)
		}
	}
	return installed, nil
}
