/*
Copyright 2022 DigitalOcean

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at:

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package crdregistration

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRegister(t *testing.T) {
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tests.example.com",
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "example.com",
			Scope: apiextensionsv1.NamespaceScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:     "tests",
				Kind:       "Test",
				ShortNames: []string{"test"},
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1",
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"},
				},
			}},
		},
	}

	crdDifferentVersion := crd.DeepCopy()
	crdDifferentVersion.Spec.Versions[0].Name = "v1beta1"

	tests := []struct {
		name        string
		crd         *apiextensionsv1.CustomResourceDefinition
		existingCRD *apiextensionsv1.CustomResourceDefinition
	}{{
		name:        "create new CRD",
		crd:         crd,
		existingCRD: nil,
	}, {
		name:        "update existing CRD",
		crd:         crd,
		existingCRD: crdDifferentVersion,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			extensionsclient := apiextensionsclient.NewSimpleClientset()
			if test.existingCRD != nil {
				extensionsclient = apiextensionsclient.NewSimpleClientset(test.existingCRD.DeepCopyObject())
			}
			client := &Client{
				apiextensionsclient: extensionsclient,
			}

			go func() {
				time.Sleep(100 * time.Millisecond)

				crd, err := client.apiextensionsclient.ApiextensionsV1().CustomResourceDefinitions().Get(context.Background(), test.crd.Name, metav1.GetOptions{})
				if err != nil {
					t.Errorf("retrieving CRD to update status: %q\n", err)
					return
				}

				crd.Status = apiextensionsv1.CustomResourceDefinitionStatus{
					Conditions: []apiextensionsv1.CustomResourceDefinitionCondition{{
						Type:   apiextensionsv1.Established,
						Status: apiextensionsv1.ConditionTrue,
					}},
				}
				_, err = client.apiextensionsclient.ApiextensionsV1().CustomResourceDefinitions().UpdateStatus(context.Background(), crd, metav1.UpdateOptions{})
				if err != nil {
					t.Errorf("updating CRD status: %q\n", err)
				}
			}()

			if err := client.Register(context.Background(), test.crd); err != nil {
				t.Error(err)
			}

			crd, err := client.apiextensionsclient.ApiextensionsV1().CustomResourceDefinitions().Get(context.Background(), test.crd.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("retrieving CRD: %q\n", err)
			}

			if diff := cmp.Diff(test.crd.Spec, crd.Spec); diff != "" {
				t.Errorf("(-want, +got)\n%s", diff)
			}
		})
	}
}
