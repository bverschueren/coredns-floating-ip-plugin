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
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

const PLUGIN_TAG_IDENTIFIER = "coredns:plugin:ospfip"

type OspFip struct {
	client    *OpenStackClient
	Origins   []string
	zones     map[string]*file.Zone
	zoneNames []string
	refresh   time.Duration
	ttl       uint32
	Next      plugin.Handler
	mutex     sync.RWMutex
}

type zone struct {
	name string
	fmap map[string]net.IP
}

func New(client *OpenStackClient, refresh time.Duration, ttl uint32) *OspFip {
	return &OspFip{
		client:    client,
		zones:     make(map[string]*file.Zone),
		zoneNames: make([]string, 0),
		refresh:   refresh,
		ttl:       ttl,
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

	if zName == "" {
		return plugin.NextOrFailure(of.Name(), of.Next, ctx, w, r)
	}

	of.mutex.Lock()
	z, ok := of.zones[zName]
	of.mutex.Unlock()
	if !ok || z == nil {
		return dns.RcodeServerFailure, nil
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	switch state.QType() {
	case dns.TypePTR:
		// TODO: reverse lookup
		log.Debugf("PTR is not implemented at this time")
		return plugin.NextOrFailure(of.Name(), of.Next, ctx, w, r)
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

	for _, fip := range taggedFips {
		ip := net.ParseIP(fip.FloatingIP)
		if ip == nil {
			log.Errorf("failed to parse IP '%s': %s.\n", fip.FloatingIP, err)
			continue
		}

		record := recordFromTags(fip.Tags)
		recordName := plugin.Name(string(record)).Normalize()
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
	}
	of.mutex.Lock()
	of.zones = zones
	of.zoneNames = zoneNames
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

// return the dns type for a given IP v4 or v6
func aType(addr net.IP) string {
	if addr.To4() != nil {
		return "A"
	} else {
		return "AAAA"
	}
	return ""
}
