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
		},
		expectedConfig: &rootConfig{
			metricsAddr:   ":9000",
			podTags:       []string{"a", "b"},
			serviceTags:   nil,
			podLabels:     map[string]bool{"foo": true, "bar": true},
			serviceLabels: map[string]bool{"baz": true},
			clusterDomain: "example.com",
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
		},
		expectedConfig: &rootConfig{
			metricsAddr:   ":9000",
			podTags:       []string{"a", "b"},
			serviceTags:   nil,
			podLabels:     map[string]bool{"foo": true, "bar": true},
			serviceLabels: map[string]bool{"baz": true},
			clusterDomain: "example.com",
		},
	}, {
		name: "flags override env vars",
		envvars: map[string]string{
			"METRICS_ADDR":       ":9009",
			"POD_IP_TAGS":        "a",
			"POD_PUBLISH_LABELS": "foo1,bar1",
		},
		flags: map[string]string{
			"metrics-addr":           ":9000",
			"pod-ip-tags":            "a,b",
			"service-ip-tags":        "",
			"pod-publish-labels":     "foo,bar",
			"service-publish-labels": "baz",
			"cluster-domain":         "example.com",
		},
		expectedConfig: &rootConfig{
			metricsAddr:   ":9000",
			podTags:       []string{"a", "b"},
			serviceTags:   nil,
			podLabels:     map[string]bool{"foo": true, "bar": true},
			serviceLabels: map[string]bool{"baz": true},
			clusterDomain: "example.com",
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

func TestRequiredConfiguration(t *testing.T) {
	tests := []struct {
		name              string
		envvars           map[string]string
		flags             map[string]string
		expectedErrSubstr string
	}{{
		name: "no netbox token provided",
		envvars: map[string]string{
			"NETBOX_API_URL": "foo",
		},
		expectedErrSubstr: flagNetBoxToken,
	}, {
		name: "no netbox API URL provided",
		envvars: map[string]string{
			"NETBOX_TOKEN": "foo",
		},
		expectedErrSubstr: flagNetBoxAPIURL,
	}, {
		name: "tag regexp",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := newRootCommand()

			for key, value := range test.envvars {
				t.Setenv(key, value)
			}
			for key, value := range test.flags {
				cmd.Flags().Set(key, value)
			}

			err := cmd.Execute()

			if err == nil {
				t.Errorf("expected error with validating %s but got a nil error", test.expectedErrSubstr)
			} else if !strings.Contains(err.Error(), test.expectedErrSubstr) {
				t.Errorf("expected error referencing %q but got %q", test.expectedErrSubstr, err)
			}
		})
	}
}

func TestTagValidation(t *testing.T) {
	tests := []struct {
		name                string
		flags               map[string]string
		expectedPodTags     []string
		expectedServiceTags []string
	}{{
		name: "service tags only",
		flags: map[string]string{
			flagPodIPTags: " foo,   bar, baz,buz",
		},
		expectedPodTags:     []string{"foo", "bar", "baz", "buz"},
		expectedServiceTags: []string{},
	}, {
		name: "pod tags only",
		flags: map[string]string{
			flagServiceIPTags: "foo,bar , baz ,buz",
		},
		expectedPodTags:     []string{},
		expectedServiceTags: []string{"foo", "bar", "baz", "buz"},
	}, {
		name: "service and pod tags",
		flags: map[string]string{
			flagPodIPTags: "foo,bar,baz,buz",
			flagServiceIPTags: "	foo,bar , baz ,buz",
		},
		expectedPodTags:     []string{"foo", "bar", "baz", "buz"},
		expectedServiceTags: []string{"foo", "bar", "baz", "buz"},
	}, {
		name: "errant commas",
		flags: map[string]string{
			flagPodIPTags:     ",foo,bar,baz,buz,",
			flagServiceIPTags: "foo,,bar,baz,buz",
		},
		expectedPodTags:     []string{"foo", "bar", "baz", "buz"},
		expectedServiceTags: []string{"foo", "bar", "baz", "buz"},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := newRootCommand()
			cfg := rootConfig{}

			for key, value := range test.flags {
				cmd.Flags().Set(key, value)
			}

			cfg.setup(cmd)

			err := compareTags(flagPodIPTags, test.expectedPodTags, cfg.podTags)
			if err != nil {
				t.Error(err)
			}
			err = compareTags(flagServiceIPTags, test.expectedServiceTags, cfg.serviceTags)
			if err != nil {
				t.Error(err)
			}
		})
	}
}

func compareTags(tagType string, expected []string, actual []string) error {
	for i, tag := range expected {
		if actual[i] != tag {
			return fmt.Errorf("expected %s %v but got %v", tagType, expected, actual)
		}
	}
	return nil
}
