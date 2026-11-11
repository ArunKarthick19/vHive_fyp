// MIT License
//
// Copyright (c) 2023 Georgiy Lebedev, Plamen Petrov, Nathaniel Tornow and vHive team
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package guest

import (
	"reflect"
	"strings"
	"testing"
)

func TestPodConfigRoundTrip(t *testing.T) {
	in := PodConfig{Env: []string{"A=1", "B=with spaces and = signs", "K_REVISION=x-00001"}, Hostname: "pod"}
	data, err := EncodePodConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(data)%ConfigDriveAlign != 0 || data[len(data)-1] != 0 {
		t.Fatalf("config drive is %d bytes, want a padded multiple of %d", len(data), ConfigDriveAlign)
	}
	out, err := DecodePodConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("got %+v, want %+v", out, in)
	}
}

func TestPodConfigExactlyAlignedGetsPadding(t *testing.T) {
	// A JSON document whose length is a multiple of the alignment still needs
	// a NUL after it, or the decoder would read past it into nothing.
	env := strings.Repeat("x", ConfigDriveAlign-len(`{"env":[""]}`))
	data, err := EncodePodConfig(PodConfig{Env: []string{env}})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 2*ConfigDriveAlign {
		t.Fatalf("len %d", len(data))
	}
	if _, err := DecodePodConfig(data); err != nil {
		t.Fatal(err)
	}
}

func TestDecodePodConfigErrors(t *testing.T) {
	for _, in := range [][]byte{nil, make([]byte, 512), []byte("{not json")} {
		if _, err := DecodePodConfig(in); err == nil {
			t.Errorf("DecodePodConfig(%q) succeeded", in)
		}
	}
}
