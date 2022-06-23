package netbox

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/zap"
)

func TestFieldsFromKeysAndValues(t *testing.T) {
	tests := []struct {
		name           string
		keysAndValues  []interface{}
		expectedFields []zap.Field
	}{{
		name: "empty",
	}, {
		name:           "simple string pair",
		keysAndValues:  []interface{}{"foo", "bar"},
		expectedFields: []zap.Field{zap.Any("foo", "bar")},
	}, {
		name:           "multiple pairs",
		keysAndValues:  []interface{}{"foo", 1, "bar", true},
		expectedFields: []zap.Field{zap.Any("foo", 1), zap.Any("bar", true)},
	}, {
		name:           "key without value",
		keysAndValues:  []interface{}{"foo", "bar", "baz"},
		expectedFields: []zap.Field{zap.Any("foo", "bar")},
	}, {
		name:          "key is not a string",
		keysAndValues: []interface{}{100, "bar"},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fields := fieldsFromKeysAndValues(test.keysAndValues)

			if diff := cmp.Diff(test.expectedFields, fields); diff != "" {
				t.Errorf("\n (-want, +got)\n%s", diff)
			}
		})
	}
}
