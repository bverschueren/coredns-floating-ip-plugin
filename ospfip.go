package ospfip

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

const PLUGIN_TAG_IDENTIFIER = "coredns:plugin:ospfip"

type OspFip struct {
	client  *OpenStackClient
	records map[string]net.IP
	refresh time.Duration
	Next    plugin.Handler
	mutex   sync.RWMutex
}

func New(client *OpenStackClient, refresh time.Duration) *OspFip {
	return &OspFip{
		client:  client,
		records: make(map[string]net.IP),
		refresh: refresh,
	}
}

func (of OspFip) Run(ctx context.Context) error {
	if err := of.updateRecords(); err != nil {
		return err
	}
	// extract zones and notify we stop updating those
	// TODO: move to gofunc to include newly added zones too
	zoneNames := make([]string, len(of.records))
	i := 0
	for zone := range of.records {
		zoneNames[i] = zone
		i++
	}
	go func() {
		timer := time.NewTimer(of.refresh)
		defer timer.Stop()
		for {
			timer.Reset(of.refresh)
			select {
			case <-ctx.Done():
				log.Debugf("stop updating records for %v: %v", zoneNames, ctx.Err())
				return
			case <-timer.C:
				if err := of.updateRecords(); err != nil && ctx.Err() == nil {
					log.Errorf("Failed to update zones %v: %v", zoneNames, err)
				}
			}
		}
	}()
	return nil
}

func (of OspFip) Name() string { return PLUGIN_NAME }

func (of OspFip) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	answer := new(dns.Msg)
	answer.SetReply(r)
	answer.Authoritative = true
	qname := state.Name()

	switch state.QType() {
	case dns.TypePTR:
		// TODO: reverse lookup
		log.Debugf("PTR is not implemented at this time")
		return plugin.NextOrFailure(of.Name(), of.Next, ctx, w, r)
	case dns.TypeA:
		if ip, found := of.records[qname]; found {
			a := new(dns.A)
			a.Hdr = dns.RR_Header{Name: qname, Rrtype: dns.TypeA, Class: dns.ClassINET}
			a.A = ip
			answer.Answer = []dns.RR{a}
		}
	case dns.TypeAAAA:
		if ip, found := of.records[qname]; found {
			a := new(dns.AAAA)
			a.Hdr = dns.RR_Header{Name: qname, Rrtype: dns.TypeAAAA, Class: dns.ClassINET}
			a.AAAA = ip
			answer.Answer = []dns.RR{a}
		}
	}

	if len(answer.Answer) == 0 {
		return plugin.NextOrFailure(of.Name(), of.Next, ctx, w, r)
	}

	w.WriteMsg(answer)
	return dns.RcodeSuccess, nil
}

func entriesFromTag(entries map[string]net.IP, fip net.IP, tags []string) {
	for _, tag := range tags {
		log.Debugf("processing fip '%s' tagged '%s'\n", fip, tag)
		// skip the identified tag
		if tag == PLUGIN_TAG_IDENTIFIER {
			continue
		}
		// extract the domain prededed by the known identifier
		domain := strings.ReplaceAll(tag, PLUGIN_TAG_IDENTIFIER+":", "")
		log.Debugf("validating if '%s' is a domain name.", domain)
		if err := validation.IsFullyQualifiedDomainName(field.NewPath(""), domain); err == nil {
			entries[domain] = fip
			// stop processing after we found a domain
			break
		} else if err := validation.IsWildcardDNS1123Subdomain(domain); err == nil {
			entries[domain] = fip
			// stop processing after we found a domain
			break
		} else {
			log.Debugf("'%s' is not a valid domain name\n", domain)
			continue
		}
	}
}

func (of *OspFip) updateRecords() error {
	taggedFips, err := of.client.ListTaggedFips(PLUGIN_TAG_IDENTIFIER)
	if err != nil {
		return err
	}
	for _, fip := range taggedFips {
		ip := net.ParseIP(fip.FloatingIP)
		if ip == nil {
			log.Errorf("failed to parse IP '%s': %s.\n", fip.FloatingIP, err)
			continue
		}
		of.mutex.Lock()
		entriesFromTag(of.records, ip, fip.Tags)
		of.mutex.Unlock()
	}
	return nil
}
