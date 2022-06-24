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

package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestConfigSetup(t *testing.T) {
	tests := []struct {
		name           string
		envvars        map[string]string
		flags          map[string]string
		expectedConfig *rootConfig
	}{{
		name: "from env vars",
		envvars: map[string]string{
			"METRICS_ADDR":           ":9000",
			"POD_IP_TAGS":            "a,b",
			"SERVICE_IP_TAGS":        " ",
			"POD_PUBLISH_LABELS":     "foo, bar",
			"SERVICE_PUBLISH_LABELS": "baz",
			"CLUSTER_DOMAIN":         "example.com",
			"READY_CHECK_ADDR":       ":4000",
		},
		expectedConfig: &rootConfig{
			metricsAddr:    ":9000",
			podTags:        []string{"a", "b"},
			serviceTags:    nil,
			podLabels:      map[string]bool{"foo": true, "bar": true},
			serviceLabels:  map[string]bool{"baz": true},
			clusterDomain:  "example.com",
			readyCheckAddr: ":4000",
		},
	}, {
		name: "from flags",
		flags: map[string]string{
			"metrics-addr":           ":9000",
			"pod-ip-tags":            "a,b",
			"service-ip-tags":        "",
			"pod-publish-labels":     "foo, bar",
			"service-publish-labels": "baz",
			"cluster-domain":         "example.com",
			"ready-check-addr":       ":4000",
		},
		expectedConfig: &rootConfig{
			metricsAddr:    ":9000",
			podTags:        []string{"a", "b"},
			serviceTags:    nil,
			podLabels:      map[string]bool{"foo": true, "bar": true},
			serviceLabels:  map[string]bool{"baz": true},
			clusterDomain:  "example.com",
			readyCheckAddr: ":4000",
		},
	}, {
		name: "flags override env vars",
		envvars: map[string]string{
			"METRICS_ADDR":       ":9009",
			"POD_IP_TAGS":        "a",
			"POD_PUBLISH_LABELS": "foo1,bar1",
			"READY_CHECK_ADDR":   ":4000",
		},
		flags: map[string]string{
			"metrics-addr":           ":9000",
			"pod-ip-tags":            "a,b",
			"service-ip-tags":        "",
			"pod-publish-labels":     "foo,bar",
			"service-publish-labels": "baz",
			"cluster-domain":         "example.com",
			"ready-check-addr":       ":5000",
		},
		expectedConfig: &rootConfig{
			metricsAddr:    ":9000",
			podTags:        []string{"a", "b"},
			serviceTags:    nil,
			podLabels:      map[string]bool{"foo": true, "bar": true},
			serviceLabels:  map[string]bool{"baz": true},
			clusterDomain:  "example.com",
			readyCheckAddr: ":5000",
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			registerRootFlags(cmd)

			for key, value := range test.envvars {
				t.Setenv(key, value)
			}
			for key, value := range test.flags {
				cmd.Flags().Set(key, value)
			}

			cfg := &rootConfig{}
			cfg.setup(cmd)

			if !reflect.DeepEqual(test.expectedConfig, cfg) {
				t.Errorf("want %v\n got %v\n", test.expectedConfig, cfg)
			}
		})
	}
}

func TestGlobalConfigValidation(t *testing.T) {
	tests := []struct {
		name              string
		netboxAPIURL      string
		netboxToken       string
		errorExpected     bool
		expectedErrSubstr string
	}{{
		name:              "no netbox token provided",
		netboxAPIURL:      "foo",
		errorExpected:     true,
		expectedErrSubstr: flagNetBoxToken,
	}, {
		name:              "no netbox API URL provided",
		netboxToken:       "foo",
		errorExpected:     true,
		expectedErrSubstr: flagNetBoxAPIURL,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := globalConfig{
				netboxAPIURL: test.netboxAPIURL,
				netboxToken:  test.netboxToken,
			}

			err := cfg.validate()

			err = expectError(test.expectedErrSubstr, err)
			if err != nil {
				t.Error(err)
			}
		})
	}
}

func TestRootConfigValidation(t *testing.T) {
	tests := []struct {
		name              string
		podLabels         map[string]bool
		serviceLabels     map[string]bool
		errorExpected     bool
		expectedErrSubstr string
	}{{
		name: "invalid pod label",
		podLabels: map[string]bool{
			"I'm simply a bad label!": true,
			"butThisIsFine":           true,
		},
		errorExpected:     true,
		expectedErrSubstr: flagPodPublishLabels,
	}, {
		name: "invalid service label",
		serviceLabels: map[string]bool{
			"_cantStartWithAnUnderscore": true,
		},
		errorExpected:     true,
		expectedErrSubstr: flagServicePublishLabels,
	}, {
		name: "valid labels",
		podLabels: map[string]bool{
			"my.domain.io/label": true,
			"1_great_label":      true,
		},
		serviceLabels: map[string]bool{
			"a-better-label": true,
			"the_best_label": true,
		},
		errorExpected: false,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := rootConfig{
				podLabels:     test.podLabels,
				serviceLabels: test.serviceLabels,
			}

			err := cfg.validate()

			if test.errorExpected {
				err = expectError(test.expectedErrSubstr, err)
				if err != nil {
					t.Error(err)
				}
			} else {
				if err != nil {
					t.Errorf("expected nil error but got %v", err)
				}
			}
		})
	}
}

func TestSanitizeStringSlices(t *testing.T) {
	tests := []struct {
		name         string
		flags        map[string]string
		inputTags    string
		expectedTags []string
	}{{
		name:         "no whitespace",
		inputTags:    "foo,bar,baz,buz",
		expectedTags: []string{"foo", "bar", "baz", "buz"},
	}, {
		name:         "lots of whitespace",
		inputTags:    " foo, bar \n, baz\t, buz ",
		expectedTags: []string{"foo", "bar", "baz", "buz"},
	}, {
		name:         "errant commas",
		inputTags:    ",foo,bar,,baz,buz,,",
		expectedTags: []string{"foo", "bar", "baz", "buz"},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for i, tag := range sanitizedStringSlice(test.inputTags) {
				if test.expectedTags[i] != tag {
					t.Errorf("expected %v but got %v", test.expectedTags, tag)
				}
			}
		})
	}
}

// expectError returns nil if the given err is non-nil and contains substr,
// else it returns an error.
func expectError(subStr string, err error) error {
	if err == nil {
		return fmt.Errorf("expected error with validating %s but got a nil error", subStr)
	} else if !strings.Contains(err.Error(), subStr) {
		return fmt.Errorf("expected error referencing %q but got %q", subStr, err)
	}
	return nil
}
