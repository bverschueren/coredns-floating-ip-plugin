package ospfip

import (
	"context"
	"net"
	"reflect"
	"testing"

	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"
	"github.com/miekg/dns"
)

func TestEntriesFromTag(t *testing.T) {
	emptyEntries := make(map[string]net.IP)

	tests := []struct {
		name     string
		fip      net.IP
		tags     []string
		expected map[string]net.IP
	}{
		{
			"single identifier tag w/o domain",
			net.IPv4(192, 168, 0, 1),
			[]string{"coredns:plugin:ospfip"},
			emptyEntries,
		},
		{
			"tag with valid fqdn",
			net.IPv4(192, 168, 0, 1),
			[]string{"coredns:plugin:ospfip:api.mycluster.example.net"},
			map[string]net.IP{"api.mycluster.example.net": net.IPv4(192, 168, 0, 1)},
		},
		{
			"tag with valid wildcard",
			net.IPv4(192, 168, 0, 1),
			[]string{"coredns:plugin:ospfip:*.mycluster.example.net"},
			map[string]net.IP{"*.mycluster.example.net": net.IPv4(192, 168, 0, 1)},
		},
		{
			"tag with valid fqdn and wildcard",
			net.IPv4(192, 168, 0, 1),
			[]string{"coredns:plugin:ospfip:api.mycluster.example.net", "coredns:plugin:ospfip:*.mycluster.example.net"},
			map[string]net.IP{"api.mycluster.example.net": net.IPv4(192, 168, 0, 1)},
		},
		{
			"tag with invalid domain",
			net.IPv4(192, 168, 0, 1),
			[]string{"coredns:plugin:ospfip:example_net"},
			emptyEntries,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := make(map[string]net.IP)
			entriesFromTag(got, tt.fip, tt.tags)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %s, want %s", got, tt.expected)
			}
		})
	}
}

func TestServeDNS(t *testing.T) {
	cases := []struct {
		Name   string
		Qname  string
		Qtype  uint16
		Rcode  int
		Answer []dns.RR
	}{
		{
			Name:  "known A record returns IPv4",
			Qname: "testipv4.example.", Qtype: dns.TypeA,
			Rcode: dns.RcodeSuccess,
			Answer: []dns.RR{
				test.A("testipv4.example.	0	IN	A	192.168.0.1"),
			},
		},
		{
			Name:  "known AAAA record returns IPv6",
			Qname: "testipv6.example.", Qtype: dns.TypeAAAA,
			Rcode: dns.RcodeSuccess,
			Answer: []dns.RR{
				test.AAAA("testipv6.example.  0       IN      AAAA       1234:abcd::1"),
			},
		},
		{
			Name:  "unknown A record calls Next Handler",
			Qname: "test.example.net", Qtype: dns.TypeAAAA,
			Rcode:  dns.RcodeNameError,
			Answer: []dns.RR{},
		},
	}

	of := OspFip{
		client: &OpenStackClient{},
		records: map[string]net.IP{
			"testipv4.example.": net.IP{192, 168, 0, 1},
			"testipv6.example.": net.ParseIP("1234:abcd::1"),
		},
		Next: test.NextHandler(dns.RcodeNameError, nil),
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {

			ctx := context.TODO()

			w := dnstest.NewRecorder(&test.ResponseWriter{})

			r := new(dns.Msg)
			r.SetQuestion(tt.Qname, tt.Qtype)

			rc, err := of.ServeDNS(ctx, w, r)
			if err != nil {
				t.Fatal(err)
			}

			if reflect.DeepEqual(tt.Answer, []dns.RR{}) {
				// no match returns empty Msg, so check the function return code instead
				if rc != tt.Rcode {
					t.Fatalf("expected ServeDNS to return %v, got %v", tt.Rcode, rc)
				}
			} else {
				if w.Msg.Rcode != tt.Rcode {
					t.Fatalf("expected rcode %v, got %v", tt.Rcode, w.Msg.Rcode)
				}
				if w.Msg.Rcode == dns.RcodeSuccess {
					if w.Msg.Answer[0].String() != tt.Answer[0].String() {
						t.Fatalf("expected answer %v, got %v", tt.Answer, w.Msg.Answer)
					}
				}
			}
		})
	}
}
