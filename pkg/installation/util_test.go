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
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/GoogleContainerTools/krew/pkg/index"

	"k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_osArch_default(t *testing.T) {
	inOS, inArch := runtime.GOOS, runtime.GOARCH
	outOS, outArch := osArch()
	if inOS != outOS {
		t.Fatalf("returned OS=%q; expected=%q", outOS, inOS)
	}
	if inArch != outArch {
		t.Fatalf("returned Arch=%q; expected=%q", outArch, inArch)
	}
}
func Test_osArch_override(t *testing.T) {
	customOS, customArch := "dragons", "v1"
	os.Setenv("KREW_OS", customOS)
	defer os.Unsetenv("KREW_OS")
	os.Setenv("KREW_ARCH", customArch)
	defer os.Unsetenv("KREW_ARCH")

	outOS, outArch := osArch()
	if customOS != outOS {
		t.Fatalf("returned OS=%q; expected=%q", outOS, customOS)
	}
	if customArch != outArch {
		t.Fatalf("returned Arch=%q; expected=%q", outArch, customArch)
	}
}

func Test_matchPlatformToSystemEnvs(t *testing.T) {
	matchingPlatform := index.Platform{
		Head: "A",
		Selector: &v1.LabelSelector{
			MatchLabels: map[string]string{
				"os": "foo",
			},
		},
		Files: nil,
	}

	tests := []struct {
		name         string
		args         []index.Platform
		wantPlatform index.Platform
		wantFound    bool
		wantErr      bool
	}{
		{
			name: "Test Matching Index",
			args: []index.Platform{
				matchingPlatform, {
					Head: "B",
					Selector: &v1.LabelSelector{
						MatchLabels: map[string]string{
							"os": "None",
						},
					},
				},
			},
			wantPlatform: matchingPlatform,
			wantFound:    true,
			wantErr:      false,
		}, {
			name: "Test Matching Index Not Found",
			args: []index.Platform{
				{
					Head: "B",
					Selector: &v1.LabelSelector{
						MatchLabels: map[string]string{
							"os": "None",
						},
					},
				},
			},
			wantPlatform: index.Platform{},
			wantFound:    false,
			wantErr:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPlatform, gotFound, err := matchPlatformToSystemEnvs(tt.args, "foo", "amdBar")
			if (err != nil) != tt.wantErr {
				t.Errorf("GetMatchingPlatform() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotPlatform, tt.wantPlatform) {
				t.Errorf("GetMatchingPlatform() gotPlatform = %v, want %v", gotPlatform, tt.wantPlatform)
			}
			if gotFound != tt.wantFound {
				t.Errorf("GetMatchingPlatform() gotFound = %v, want %v", gotFound, tt.wantFound)
			}
		})
	}
}

func Test_choosePluginVersion(t *testing.T) {
	type args struct {
		p         index.Platform
		forceHEAD bool
	}
	tests := []struct {
		name        string
		args        args
		wantVersion string
		wantURI     string
		wantErr     bool
	}{
		{
			name: "Get Single Head",
			args: args{
				p: index.Platform{
					Head:   "https://head.git",
					URI:    "",
					Sha256: "",
				},
				forceHEAD: false,
			},
			wantVersion: "HEAD",
			wantURI:     "https://head.git",
		}, {
			name: "Get URI default",
			args: args{
				p: index.Platform{
					Head:   "https://head.git",
					URI:    "https://uri.git",
					Sha256: "deadbeef",
				},
				forceHEAD: false,
			},
			wantVersion: "deadbeef",
			wantURI:     "https://uri.git",
		}, {
			name: "Get HEAD force",
			args: args{
				p: index.Platform{
					Head:   "https://head.git",
					URI:    "https://uri.git",
					Sha256: "deadbeef",
				},
				forceHEAD: true,
			},
			wantVersion: "HEAD",
			wantURI:     "https://head.git",
		}, {
			name: "HEAD force fallback",
			args: args{
				p: index.Platform{
					Head:   "",
					URI:    "https://uri.git",
					Sha256: "deadbeef",
				},
				forceHEAD: true,
			},
			wantErr:     true,
			wantVersion: "",
			wantURI:     "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVersion, gotURI, err := choosePluginVersion(tt.args.p, tt.args.forceHEAD)
			if (err != nil) != tt.wantErr {
				t.Errorf("choosePluginVersion() gotVersion = %v, want %v, got err = %v want err = %v", gotVersion, tt.wantVersion, err, tt.wantErr)
			}
			if gotVersion != tt.wantVersion {
				t.Errorf("choosePluginVersion() gotVersion = %v, want %v", gotVersion, tt.wantVersion)
			}
			if gotURI != tt.wantURI {
				t.Errorf("choosePluginVersion() gotURI = %v, want %v", gotURI, tt.wantURI)
			}
		})
	}
}

func Test_findInstalledPluginVersion(t *testing.T) {
	type args struct {
		installPath string
		binDir      string
		pluginName  string
	}
	tests := []struct {
		name          string
		args          args
		wantName      string
		wantInstalled bool
		wantErr       bool
	}{
		{
			name: "Find version",
			args: args{
				installPath: filepath.Join(testdataPath(t), "index"),
				binDir:      filepath.Join(testdataPath(t), "bin"),
				pluginName:  "foo",
			},
			wantName:      "deadbeef",
			wantInstalled: true,
			wantErr:       false,
		}, {
			name: "No installed version",
			args: args{
				installPath: filepath.Join(testdataPath(t), "index"),
				binDir:      filepath.Join(testdataPath(t), "bin"),
				pluginName:  "not-found",
			},
			wantName:      "",
			wantInstalled: false,
			wantErr:       false,
		}, {
			name: "Insecure name",
			args: args{
				installPath: filepath.Join(testdataPath(t), "index"),
				binDir:      filepath.Join(testdataPath(t), "bin"),
				pluginName:  "../foo",
			},
			wantName:      "",
			wantInstalled: false,
			wantErr:       true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotInstalled, err := findInstalledPluginVersion(tt.args.installPath, tt.args.binDir, tt.args.pluginName)
			if (err != nil) != tt.wantErr {
				t.Errorf("getOtherInstalledVersion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotName != tt.wantName {
				t.Errorf("getOtherInstalledVersion() gotName = %v, want %v", gotName, tt.wantName)
			}
			if gotInstalled != tt.wantInstalled {
				t.Errorf("getOtherInstalledVersion() gotInstalled = %v, want %v", gotInstalled, tt.wantInstalled)
			}
		})
	}
}

func testdataPath(t *testing.T) string {
	pwd, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(pwd, "testdata")
}

func Test_pluginVersionFromPath(t *testing.T) {
	type args struct {
		installPath string
		pluginPath  string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "normal version",
			args: args{
				installPath: filepath.FromSlash("install/"),
				pluginPath:  filepath.FromSlash("install/foo/HEAD/kubectl-foo"),
			},
			want:    "HEAD",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pluginVersionFromPath(tt.args.installPath, tt.args.pluginPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("pluginVersionFromPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("pluginVersionFromPath() = %v, want %v", got, tt.want)
			}
		})
	}
}
