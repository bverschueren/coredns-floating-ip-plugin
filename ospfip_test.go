package ospfip

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/coredns/coredns/plugin/file"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"
	fake "github.com/gophercloud/gophercloud/openstack/networking/v2/common"
	th "github.com/gophercloud/gophercloud/testhelper"
	"github.com/miekg/dns"
)

func TestRecordFromTags(t *testing.T) {

	tests := []struct {
		name     string
		tags     []string
		expected string
	}{
		{
			"tag w/o valid identifier",
			[]string{"abc123"},
			"",
		},
		{
			"single identifier tag w/o domain",
			[]string{"coredns:plugin:ospfip"},
			"",
		},
		{
			"tag with valid fqdn",
			[]string{"coredns:plugin:ospfip:api.mycluster.example.net"},
			"api.mycluster.example.net",
		},
		{
			"tag with valid wildcard",
			[]string{"coredns:plugin:ospfip:*.mycluster.example.net"},
			"*.mycluster.example.net",
		},
		{
			"tag with multiple valid fqdn selects one",
			[]string{"coredns:plugin:ospfip:api.mycluster.example.net", "coredns:plugin:ospfip:*.mycluster.example.net"},
			"api.mycluster.example.net",
		},
		{
			"tag with invalid domain",
			[]string{"coredns:plugin:ospfip:example_net"},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recordFromTags(tt.tags)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %s, want %s", got, tt.expected)
			}
		})
	}
}

func TestServeDNS(t *testing.T) {
	cases := []struct {
		name       string
		zoneName   string
		recordName string
		ip         net.IP
		qname      string
		qtype      uint16
		rcode      int
		answer     []dns.RR
	}{
		{
			name:       "known A record returns IPv4",
			zoneName:   "example.",
			recordName: "testipv4",
			ip:         net.IP{192, 168, 0, 1},
			qname:      "testipv4.example.", qtype: dns.TypeA,
			rcode: dns.RcodeSuccess,
			answer: []dns.RR{
				test.A("testipv4.example.	0	IN	A	192.168.0.1"),
			},
		},
		{
			name:       "known A record for wildcard domain returns IPv4",
			zoneName:   "example.",
			recordName: "*",
			ip:         net.IP{192, 168, 0, 1},
			qname:      "sub.example.", qtype: dns.TypeA,
			rcode: dns.RcodeSuccess,
			answer: []dns.RR{
				test.A("sub.example.	0	IN	A	192.168.0.1"),
			},
		},
		{
			name:       "known AAAA record returns IPv6",
			zoneName:   "example.",
			recordName: "testipv6",
			ip:         net.ParseIP("1234:abcd::1"),
			qname:      "testipv6.example.", qtype: dns.TypeAAAA,
			rcode: dns.RcodeSuccess,
			answer: []dns.RR{
				test.AAAA("testipv6.example.  0       IN      AAAA       1234:abcd::1"),
			},
		},
		{
			name:       "known IPv4 addres returns PTR",
			zoneName:   "example.",
			recordName: "testipv4",
			ip:         net.IP{192, 168, 0, 1},
			qname:      "1.0.168.192.in-addr.arpa.", qtype: dns.TypePTR,
			rcode: dns.RcodeSuccess,
			answer: []dns.RR{
				test.PTR("1.0.168.192.in-addr.arpa.  0       IN      PTR       testipv4.example."),
			},
		},
		{
			name:       "known IPv6 record returns PTR",
			zoneName:   "example.",
			recordName: "testipv6",
			ip:         net.ParseIP("1234:abcd::1"),
			qname:      "1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.d.c.b.a.4.3.2.1.ip6.arpa.", qtype: dns.TypePTR,
			rcode: dns.RcodeSuccess,
			answer: []dns.RR{
				test.PTR("1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.d.c.b.a.4.3.2.1.ip6.arpa.  0       IN      PTR       testipv6.example."),
			},
		},
		{
			name:       "unknown A record calls Next Handler",
			zoneName:   "example.",
			recordName: "unknown",
			ip:         net.ParseIP("1234:abcd::1"),
			qname:      "test.example.net", qtype: dns.TypeAAAA,
			rcode:  dns.RcodeNameError,
			answer: []dns.RR{},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {

			zone := file.NewZone(tt.zoneName, "")
			rfc1035 := fmt.Sprintf("%s %d IN %s %s", dns.Fqdn(tt.recordName+"."+tt.zoneName), 0, aType(tt.ip), tt.ip)
			rr, _ := dns.NewRR(rfc1035)
			soa := soaFromOrigin(tt.zoneName, 3600)
			zone.Insert(soa[0])
			zone.Insert(rr)

			of := OspFip{
				zones: map[string]*file.Zone{
					tt.zoneName: zone,
				},
				zoneNames: []string{tt.zoneName},
				reverseRecords: map[string]string{
					tt.ip.String(): tt.recordName + "." + tt.zoneName,
				},
				Next: test.NextHandler(dns.RcodeNameError, nil),
			}
			ctx := context.TODO()

			w := dnstest.NewRecorder(&test.ResponseWriter{})

			r := new(dns.Msg)
			r.SetQuestion(tt.qname, tt.qtype)

			rc, err := of.ServeDNS(ctx, w, r)
			if err != nil {
				t.Fatal(err)
			}

			if reflect.DeepEqual(tt.answer, []dns.RR{}) {

				if rc != tt.rcode {
					t.Fatalf("expected ServeDNS to return %v, got %v", tt.rcode, rc)
				}
			} else {
				if w.Msg.Rcode != tt.rcode {
					t.Fatalf("expected rcode %v, got %v", tt.rcode, w.Msg.Rcode)
				}
				if w.Msg.Rcode == dns.RcodeSuccess {
					if w.Msg.Answer[0].String() != tt.answer[0].String() {
						t.Fatalf("expected answer %v, got %v", tt.answer, w.Msg.Answer)
					}
				}
			}
		})
	}
}

func TestUpdateRecords(t *testing.T) {
	cases := []struct {
		name                   string
		listResponse           string
		expectedZoneName       string
		expectedRecords        int
		expectedReverseRecords int
	}{
		{name: "update from tagged fips", listResponse: ListResponse(taggedFip), expectedZoneName: "mycluster.example.net.", expectedRecords: 1, expectedReverseRecords: 1},
		{name: "update from tagged wildcard fips", listResponse: ListResponse(taggedWildcardFip), expectedZoneName: "mycluster.example.net.", expectedRecords: 1, expectedReverseRecords: 0},
		{name: "update from taggless fips", listResponse: ListResponse(""), expectedZoneName: "", expectedRecords: 0, expectedReverseRecords: 0},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			th.SetupHTTP()
			defer th.TeardownHTTP()

			th.Mux.HandleFunc("/v2.0/floatingips", func(w http.ResponseWriter, r *http.Request) {
				th.TestMethod(t, r, "GET")
				th.TestHeader(t, r, "X-Auth-Token", fake.TokenID)

				w.Header().Add("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)

				fmt.Fprintf(w, tt.listResponse)
			})

			osc := &OpenStackClient{client: fake.ServiceClient()}
			refresh := 5 * time.Minute
			of := New(osc, refresh, 5)
			of.Origins = []string{"."}

			err := of.updateRecords()
			if err != nil {
				t.Errorf("failed to update records: %s", err)
			}
			zone, ok := of.zones[tt.expectedZoneName]
			if tt.expectedZoneName != "" && !ok {
				t.Fatalf("expected zone '%s', got %+v", tt.expectedZoneName, of.zones)
			}
			if tt.expectedZoneName != "" {
				if tt.expectedRecords != zone.Len() {
					t.Fatalf("expected %+v zones, got %+v", tt.expectedRecords, zone.Len())
				}
				if tt.expectedReverseRecords != len(of.reverseRecords) {
					t.Fatalf("expected %+v zones, got %+v", tt.expectedReverseRecords, len(of.reverseRecords))
				}
			}
		})
	}
}

func TestZoneFromRecord(t *testing.T) {
	cases := []struct {
		Name     string
		zone     string
		expected string
	}{
		{
			Name:     "extract zone from wildcard",
			zone:     "*.example.org",
			expected: "example.org.",
		},
		{
			Name:     "extract zone from dot-notated zone",
			zone:     ".example.org",
			expected: "example.org.",
		},
		{
			Name:     "extract zone from record",
			zone:     "a.example.org",
			expected: "example.org.",
		},
		{
			Name:     "extract zone from fqdn record",
			zone:     "a.example.org.",
			expected: "example.org.",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			got := zoneFromRecord(tt.zone)
			if got != tt.expected {
				t.Fatalf("expected %v, got %v", tt.expected, got)
			}
		})
	}

}

func TestUnFqdn(t *testing.T) {

	cases := []struct {
		Name     string
		record   string
		expected string
	}{
		{
			Name:     "non-fqdn wildcard",
			record:   "*.example.org",
			expected: "*.example.org",
		},
		{
			Name:     "fqdn wildcard",
			record:   "*.example.org.",
			expected: "*.example.org",
		},
		{
			Name:     "non-fqdn non-wildcard",
			record:   "test.example.org",
			expected: "test.example.org",
		},
		{
			Name:     "fqdn non-wildcard",
			record:   "test.example.org.",
			expected: "test.example.org",
		},
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			got := unFqdn(tt.record)
			if got != tt.expected {
				t.Fatalf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}
