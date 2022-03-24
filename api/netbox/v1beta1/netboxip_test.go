package v1beta1

import (
	"net"
	"testing"
)

func TestIPUnmarshalText(t *testing.T) {
	tests := []struct {
		name  string
		ipStr string
		ip    net.IP
		valid bool
	}{{
		name:  "empty",
		ipStr: "",
		ip:    nil,
		valid: false,
	}, {
		name:  "valid IPv4",
		ipStr: "192.168.0.1",
		ip:    net.IPv4(192, 168, 0, 1),
		valid: true,
	}, {
		name:  "valid IPv6",
		ipStr: "1:2::3",
		ip:    net.IP{0, 1, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3},
		valid: true,
	}, {
		name:  "invalid",
		ipStr: "not-an-ip",
		ip:    nil,
		valid: false,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var ip IP
			err := ip.UnmarshalText([]byte(test.ipStr))
			if err != nil && test.valid {
				t.Errorf("want no error, got %q\n", err)
			} else if err == nil && !test.valid {
				t.Error("want error, nil")
			}

			if !test.ip.Equal(net.IP(ip)) {
				t.Errorf("want %v, got %v\n", test.ip, net.IP(ip))
			}
		})
	}
}
