package ospfip

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/file"
	"github.com/coredns/coredns/plugin/pkg/dnsutil"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

const PLUGIN_TAG_IDENTIFIER = "coredns:plugin:ospfip"

type OspFip struct {
	client         *OpenStackClient
	Origins        []string
	zones          map[string]*file.Zone
	zoneNames      []string
	reverseRecords map[string]string
	refresh        time.Duration
	ttl            uint32
	Next           plugin.Handler
	mutex          sync.RWMutex
}

type zone struct {
	name string
	fmap map[string]net.IP
}

func New(client *OpenStackClient, refresh time.Duration, ttl uint32) *OspFip {
	return &OspFip{
		client:  client,
		refresh: refresh,
		ttl:     ttl,
	}
}

func (of *OspFip) Run(ctx context.Context) error {
	log.Info("Running initial update of records...")
	if err := of.updateRecords(); err != nil {
		return err
	}

	go func() {
		timer := time.NewTimer(of.refresh)
		defer timer.Stop()
		for {
			timer.Reset(of.refresh)
			select {
			case <-ctx.Done():
				log.Debugf("stop updating records for %v: %v", of.zoneNames, ctx.Err())
				return
			case <-timer.C:
				if err := of.updateRecords(); err != nil && ctx.Err() == nil {
					log.Errorf("Failed to update zones %v: %v", of.zoneNames, err)
				}
			}
		}
	}()
	return nil
}

func (of *OspFip) Name() string { return PLUGIN_NAME }

func (of *OspFip) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	qname := state.Name()

	of.mutex.Lock()
	zName := plugin.Zones(of.zoneNames).Matches(qname)
	of.mutex.Unlock()
	if zName == "" && state.QType() != dns.TypePTR {
		return plugin.NextOrFailure(of.Name(), of.Next, ctx, w, r)
	}

	of.mutex.Lock()
	z, ok := of.zones[zName]
	of.mutex.Unlock()
	if (!ok || z == nil) && state.QType() != dns.TypePTR {
		return dns.RcodeServerFailure, nil
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	switch state.QType() {
	case dns.TypePTR:
		addr := dnsutil.ExtractAddressFromReverse(qname)
		if addr == "" {
			return plugin.NextOrFailure(of.Name(), of.Next, ctx, w, r)
		}
		of.mutex.RLock()
		record := of.reverseRecords[addr]
		of.mutex.RUnlock()
		if record == "" {
			return plugin.NextOrFailure(of.Name(), of.Next, ctx, w, r)
		}
		rfc1035 := fmt.Sprintf("%s %d IN %s %s", qname, of.ttl, "PTR", dns.Fqdn(record))

		rr, err := dns.NewRR(rfc1035)
		if err != nil {
			return dns.RcodeServerFailure, fmt.Errorf("failed to parse resource record: %v", err)
		}
		m.Answer = []dns.RR{rr}
	case dns.TypeA, dns.TypeAAAA:
		of.mutex.RLock()
		m.Answer, m.Ns, m.Extra, _ = z.Lookup(ctx, state, qname)
		of.mutex.RUnlock()
	}

	if len(m.Answer) == 0 {
		return plugin.NextOrFailure(of.Name(), of.Next, ctx, w, r)
	}

	w.WriteMsg(m)
	return dns.RcodeSuccess, nil
}

func (of *OspFip) updateRecords() error {
	taggedFips, err := of.client.ListTaggedFips(PLUGIN_TAG_IDENTIFIER)
	if err != nil {
		return err
	}
	zones := make(map[string]*file.Zone)
	zoneNames := make([]string, 0)
	reverseRecords := make(map[string]string)

	for _, fip := range taggedFips {
		ip := net.ParseIP(fip.FloatingIP)
		if ip == nil {
			log.Errorf("failed to parse IP '%s': %s.\n", fip.FloatingIP, err)
			continue
		}

		recordTag := recordFromTags(fip.Tags)
		recordName := plugin.Name(string(recordTag)).Normalize()
		if plugin.Zones(of.Origins).Matches(recordName) == "" {
			log.Debugf("'%s' does not match the configured origin(s), skipping...", recordName)
			continue
		}
		zoneName := zoneFromRecord(recordName)

		rfc1035 := fmt.Sprintf("%s %d IN %s %s", dns.Fqdn(recordName), of.ttl, aType(ip), ip)
		rr, err := dns.NewRR(rfc1035)
		if err != nil {
			return fmt.Errorf("failed to parse resource record: %v", err)
		}

		zone, ok := zones[zoneName]
		if !ok || zone == nil {
			zone = file.NewZone(zoneName, "")
			zoneNames = append(zoneNames, zoneName)

			soa := soaFromOrigin(zoneName, of.ttl)
			err = zone.Insert(soa[0])
			if err != nil {
				return fmt.Errorf("failed to insert record: %v", err)
			}
		}

		zone.Insert(rr)
		if err != nil {
			return fmt.Errorf("failed to insert record: %v", err)
		}
		zones[zoneName] = zone
		if err := validation.IsWildcardDNS1123Subdomain(unFqdn(recordName)); err != nil {
			log.Debugf("Adding PTR record for '%s' as '%s'", ip.String(), recordName)
			reverseRecords[ip.String()] = dns.Fqdn(recordName)
		}
	}
	of.mutex.Lock()
	of.zones = zones
	of.zoneNames = zoneNames
	of.reverseRecords = reverseRecords
	of.mutex.Unlock()
	log.Debugf("currently authoritative for zones %s", of.zoneNames)
	return nil
}

// craft an soa to make sure Lookup works: https://github.com/coredns/coredns/blob/8868454177bdd3e70e71bd52d3c0e38bcf0d77fd/plugin/file/lookup.go#L44-L46
func soaFromOrigin(origin string, ttl uint32) []dns.RR {
	hdr := dns.RR_Header{Name: origin, Ttl: ttl, Class: dns.ClassINET, Rrtype: dns.TypeSOA}
	return []dns.RR{&dns.SOA{Hdr: hdr, Ns: "localhost.", Mbox: "root.localhost.", Serial: 1, Refresh: 0, Retry: 0, Expire: 0, Minttl: ttl}}
}

// return the zone part of a given record
func zoneFromRecord(in string) string {
	labels := dns.SplitDomainName(in)
	return plugin.Name(strings.Join(labels[1:], ".")).Normalize()
}

// extract a record from a list of tags
// a record only resolvaes to a single floating ip so we expect a 1:1 tag-to-zone mapping
func recordFromTags(tags []string) string {
	for _, tag := range tags {
		// skip the identified tag
		if tag == PLUGIN_TAG_IDENTIFIER {
			continue
		}
		log.Debugf("processing tag '%s'\n", tag)
		// extract the domain prededed by the known identifier
		domain := strings.ReplaceAll(tag, PLUGIN_TAG_IDENTIFIER+":", "")
		log.Debugf("validating if '%s' is a domain name", domain)
		if err := validation.IsFullyQualifiedDomainName(field.NewPath(""), domain); err == nil {
			// stop processing after we found a domain
			return domain
		} else if err := validation.IsWildcardDNS1123Subdomain(domain); err == nil {
			// stop processing after we found a domain
			return domain
		} else {
			log.Debugf("'%s' is not a valid zone\n", domain)
			continue
		}
	}
	return ""
}

// IsWildcardDNS1123Subdomain doesn't consider fqdn domains so unfqdn before validating
// https://github.com/kubernetes/apimachinery/blob/d82afe1e363acae0e8c0953b1bc230d65fdb50e2/pkg/util/validation/validation.go#L255C6-L255C32
func unFqdn(record string) string {
	suffix := "."
	if strings.HasSuffix(record, suffix) {
		record = record[:len(record)-len(suffix)]
	}
	return record
}

// return the dns type for a given IP v4 or v6
func aType(addr net.IP) string {
	if addr.To4() != nil {
		return "A"
	} else {
		return "AAAA"
	}
}
