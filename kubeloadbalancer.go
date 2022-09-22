package kubeLoadBalancer

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	core "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/dnsutil"
	"github.com/coredns/coredns/plugin/pkg/fall"
	"github.com/coredns/coredns/request"
)

type KubeLoadBalancer struct {
	Next  plugin.Handler
	Zones []string

	Fall fall.F
	ttl  uint32

	// Kubernetes API interface
	client     kubernetes.Interface
	controller cache.Controller
	indexer    cache.Indexer

	// concurrency control to stop controller
	stopLock sync.Mutex
	shutdown bool
	stopCh   chan struct{}
}

func New(zones []string) *KubeLoadBalancer {
	k := new(KubeLoadBalancer)
	k.Zones = zones
	k.ttl = defaultTTL
	k.stopCh = make(chan struct{})
	return k
}

const (
	// defaultTTL to apply to all answers.
	defaultTTL = 5
)

// Name implements the Handler interface.
func (k *KubeLoadBalancer) Name() string { return "kubeloadbalancer" }

// ServeDNS implements the plugin.Handler interface.
func (k *KubeLoadBalancer) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}

	qname := state.Name()
	zone := plugin.Zones(k.Zones).Matches(qname)
	if zone == "" {
		return plugin.NextOrFailure(k.Name(), k.Next, ctx, w, r)
	}
	zone = state.QName()[len(qname)-len(zone):] // maintain case of original query
	state.Zone = zone

	// query for just the zone results in NODATA
	if len(zone) == len(qname) {
		return k.nodata(state)
	}

	// handle lookup
	serviceDomain := state.Name()[0 : len(qname)-len(zone)-1]
	if zone == "." {
		serviceDomain = state.Name()[0 : len(qname)-len(zone)]
	}
	serviceSegments := dns.SplitDomainName(serviceDomain)

	var items []interface{}
	var err error

	switch len(serviceSegments) {
	case 2:
		// get the service by key name from the indexer
		serviceKey := strings.Join([]string{serviceSegments[1], "/", serviceSegments[0]}, "")

		items, err = k.indexer.ByIndex("namewithns", serviceKey)
		if err != nil {
			return dns.RcodeServerFailure, err
		}
	case 1:
		// query only contains the service Name
		items, err = k.indexer.ByIndex("name", serviceSegments[0])
		if err != nil {
			return dns.RcodeServerFailure, err
		}
	}

	if len(items) == 0 {
		return k.nxdomain(ctx, state)
	}

	var records []dns.RR
	for _, item := range items {
		service, ok := item.(*core.Service)
		if !ok {
			return dns.RcodeServerFailure, fmt.Errorf("unexpected %q from *Service index", reflect.TypeOf(item))
		}

		// add response records
		if state.QType() == dns.TypeA {
			for _, loadbalanceIP := range service.Status.LoadBalancer.Ingress {
				if strings.Contains(loadbalanceIP.IP, ":") {
					continue
				}
				if netIP := net.ParseIP(loadbalanceIP.IP); netIP != nil {
					records = append(records, &dns.A{A: netIP,
						Hdr: dns.RR_Header{Name: qname, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: k.ttl}})
				}
			}
		}
		if state.QType() == dns.TypeAAAA {
			for _, loadbalanceIP := range service.Status.LoadBalancer.Ingress {
				if !strings.Contains(loadbalanceIP.IP, ":") {
					continue
				}
				if netIP := net.ParseIP(loadbalanceIP.IP); netIP != nil {
					records = append(records, &dns.AAAA{AAAA: netIP,
						Hdr: dns.RR_Header{Name: qname, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: k.ttl}})
				}
			}
		}
	}

	writeResponse(w, r, records, nil, nil, dns.RcodeSuccess)
	return dns.RcodeSuccess, nil
}

func (k *KubeLoadBalancer) nxdomain(ctx context.Context, state request.Request) (int, error) {
	if k.Fall.Through(state.Name()) {
		return plugin.NextOrFailure(k.Name(), k.Next, ctx, state.W, state.Req)
	}
	writeResponse(state.W, state.Req, nil, nil, []dns.RR{k.soa()}, dns.RcodeNameError)
	return dns.RcodeNameError, nil
}

func (k *KubeLoadBalancer) nodata(state request.Request) (int, error) {
	writeResponse(state.W, state.Req, nil, nil, []dns.RR{k.soa()}, dns.RcodeSuccess)
	return dns.RcodeSuccess, nil
}

func writeResponse(w dns.ResponseWriter, r *dns.Msg, answer, extra, ns []dns.RR, rcode int) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Rcode = rcode
	m.Authoritative = true
	m.Answer = answer
	m.Extra = extra
	m.Ns = ns
	w.WriteMsg(m)
}

func (k *KubeLoadBalancer) soa() *dns.SOA {
	return &dns.SOA{
		Hdr:     dns.RR_Header{Name: k.Zones[0], Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: k.ttl},
		Ns:      dnsutil.Join("ns.dns", k.Zones[0]),
		Mbox:    dnsutil.Join("hostmaster.dns", k.Zones[0]),
		Serial:  uint32(time.Now().Unix()),
		Refresh: 3600,
		Retry:   1800,
		Expire:  86400,
		Minttl:  k.ttl,
	}
}

// Ready implements the ready.Readiness interface.
func (k *KubeLoadBalancer) Ready() bool {
	return k.controller.HasSynced()
}
