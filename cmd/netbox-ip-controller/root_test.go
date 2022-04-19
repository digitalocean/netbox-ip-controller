package main

import (
	"reflect"
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
