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

package controller

import (
	"testing"

	"github.com/digitalocean/netbox-ip-controller/internal/netbox"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestWithTags(t *testing.T) {
	tests := []struct {
		name         string
		existingTags map[string]netbox.Tag
		addedTags    []string
		expectedTags []netbox.Tag
	}{{
		name: "no tags to add",
	}, {
		name:         "no exising tags",
		addedTags:    []string{"foo"},
		expectedTags: []netbox.Tag{{Name: "foo", Slug: "foo"}},
	}, {
		name:         "exising and added tags do not overlap",
		existingTags: map[string]netbox.Tag{"bar": {Name: "bar"}},
		addedTags:    []string{"foo"},
		expectedTags: []netbox.Tag{{Name: "foo", Slug: "foo"}},
	}, {
		name: "exising and added tags overlap",
		existingTags: map[string]netbox.Tag{
			"foo": {Name: "foo", Slug: "existing-foo"},
		},
		addedTags:    []string{"foo"},
		expectedTags: []netbox.Tag{{Name: "foo", Slug: "existing-foo"}},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			netboxClient := netbox.NewFakeClient(test.existingTags, nil)

			var s Settings
			o := WithTags(test.addedTags, netboxClient)
			if err := o(&s); err != nil {
				t.Fatal(err)
			}

			diff := cmp.Diff(
				test.expectedTags,
				s.Tags,
				cmpopts.SortSlices(func(t1, t2 netbox.Tag) bool { return t1.Name < t2.Name }),
				cmpopts.IgnoreUnexported(netbox.Tag{}),
			)
			if diff != "" {
				t.Errorf("(-want, +got)\n%s", diff)
			}
		})
	}
}
