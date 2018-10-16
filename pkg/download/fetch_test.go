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
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestHTTPFetcher(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL)
	out, err := f.Get()
	if err != nil {
		t.Fatal(err)
	}
	out.Close()
	if !called {
		t.Fatal("request handler not called")
	}
}

func TestFileFetcher(t *testing.T) {
	f, err := ioutil.TempFile("", "testfile")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	expected := "hello world"
	if _, err := fmt.Fprintf(f, expected); err != nil {
		t.Fatal(err)
	}
	f.Close()

	ff := NewFileFetcher(f.Name())
	out, err := ff.Get()
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if v, err := ioutil.ReadAll(out); err != nil {
		t.Fatal(err)
	} else if string(v) != expected {
		t.Fatalf("got=%q expected=%q", string(v), expected)
	}
}
