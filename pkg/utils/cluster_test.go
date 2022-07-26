// Copyright 2020 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import (
	"encoding/base64"
	"testing"

	"github.com/onsi/gomega"
)

func TestGetManagedClusterNamespace(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	g.Expect(GetManagedClusterNamespace("")).To(gomega.Equal(""))
	g.Expect(GetManagedClusterNamespace("secretname-cluster-secret")).To(gomega.Equal("secretname"))
	g.Expect(GetManagedClusterNamespace("secretname")).To(gomega.Equal(""))
}

func TestBase64String(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	myString := base64.StdEncoding.EncodeToString([]byte("Hello world"))
	myDecodedString, err := base64.StdEncoding.DecodeString(myString)

	if err != nil {
		t.Fail()
	}

	decoded, err := Base64StringDecode(myString)
	g.Expect(decoded).To(gomega.Equal(string(myDecodedString)))
	g.Expect(err).NotTo(gomega.HaveOccurred())
}
