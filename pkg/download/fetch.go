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
	"io"
	"net/http"
	"os"
)

// Fetcher is used to get files from a URI.
type Fetcher interface {
	// Get gets the file and returns an stream to read the file.
	Get() (io.ReadCloser, error)
}

type httpFetcher struct{ url string }

// Get gets the file and returns an stream to read the file.
func (h httpFetcher) Get() (io.ReadCloser, error) {
	resp, err := http.Get(h.url)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// NewHTTPFetcher is used to get a file from a http:// or https:// schema path.
func NewHTTPFetcher(url string) Fetcher { return httpFetcher{url: url} }

type fileFetcher struct{ f string }

func (f fileFetcher) Get() (io.ReadCloser, error) {
	return os.Open(f.f)
}

// NewFileFetcher returns a local file reader.
func NewFileFetcher(path string) Fetcher { return fileFetcher{f: path} }
