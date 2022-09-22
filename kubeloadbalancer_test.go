package kubeLoadBalancer

import (
	"context"
	"testing"
	"time"

	"github.com/miekg/dns"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"
)

func TestServeDNSModeNameAndIP(t *testing.T) {
	k := New([]string{"at-cloud.md.saicmotortest.com."})

	var externalCases = []test.Case{
		{
			Qname: "loadbalancer1.at-cloud.md.saicmotortest.com.", Qtype: dns.TypeA,
			Rcode: dns.RcodeSuccess,
			Answer: []dns.RR{
				test.A("loadbalancer1.at-cloud.md.saicmotortest.com.	5	IN	A	10.91.196.11"),
			},
		},
		{
			Qname: "loadbalancer1.namespace1.at-cloud.md.saicmotortest.com.", Qtype: dns.TypeA,
			Rcode: dns.RcodeSuccess,
			Answer: []dns.RR{
				test.A("loadbalancer1.namespace1.at-cloud.md.saicmotortest.com.	5	IN	A	10.91.196.11"),
			},
		},
		{
			Qname: "loadbalancer2.at-cloud.md.saicmotortest.com.", Qtype: dns.TypeA,
			Rcode: dns.RcodeSuccess,
			Answer: []dns.RR{
				test.A("loadbalancer2.at-cloud.md.saicmotortest.com.	5	IN	A	10.91.196.11"),
				test.A("loadbalancer2.at-cloud.md.saicmotortest.com.	5	IN	A	10.91.196.12"),
			},
		},
		{
			Qname: "loadbalancer2.at-cloud.md.saicmotortest.com.", Qtype: dns.TypeAAAA,
			Rcode: dns.RcodeSuccess,
			Answer: []dns.RR{
				test.AAAA("loadbalancer2.at-cloud.md.saicmotortest.com.	5	IN	AAAA	10:91:196::11"),
				test.AAAA("loadbalancer2.at-cloud.md.saicmotortest.com.	5	IN	AAAA	10:91:196::12"),
			},
		},
		{
			Qname: "lbpending.at-cloud.md.saicmotortest.com.", Qtype: dns.TypeA,
			Rcode: dns.RcodeSuccess,
			Ns:    []dns.RR{},
		},
		{
			Qname: "abc.at-cloud.md.saicmotortest.com.", Qtype: dns.TypeA,
			Rcode: dns.RcodeNameError,
			Ns:    []dns.RR{k.soa()},
		},
		{
			Qname: "at-cloud.md.saicmotortest.com.", Qtype: dns.TypeA,
			Rcode: dns.RcodeSuccess,
			Ns:    []dns.RR{k.soa()},
		},
	}

	k.client = fake.NewSimpleClientset()
	ctx := context.Background()
	addFixtures(ctx, k)

	k.setWatch(ctx)
	go k.controller.Run(k.stopCh)
	defer close(k.stopCh)

	// quick and dirty wait for sync
	for !k.controller.HasSynced() {
		time.Sleep(100 * time.Millisecond)
	}

	runTests(t, ctx, k, externalCases)
}

func addFixtures(ctx context.Context, k *KubeLoadBalancer) {
	loadbalancer1 := &core.Service{
		ObjectMeta: meta.ObjectMeta{
			Name:        "loadbalancer1",
			Namespace:   "namespace1",
			Annotations: map[string]string{"foo": "bar", "bar": "foo"},
		},
		Status: core.ServiceStatus{
			LoadBalancer: core.LoadBalancerStatus{
				Ingress: []core.LoadBalancerIngress{
					{IP: "10.91.196.11"},
				},
			},
		},
	}
	loadbalancer2 := &core.Service{
		ObjectMeta: meta.ObjectMeta{
			Name:        "loadbalancer2",
			Namespace:   "namespace1",
			Annotations: map[string]string{"foo": "bar", "bar": "foo"},
		},
		Status: core.ServiceStatus{
			LoadBalancer: core.LoadBalancerStatus{
				Ingress: []core.LoadBalancerIngress{
					{IP: "10.91.196.11"},
					{IP: "10.91.196.12"},
					{IP: "10:91:196::11"},
					{IP: "10:91:196::12"},
				},
			},
		},
	}
	lbPending := &core.Service{
		ObjectMeta: meta.ObjectMeta{
			Name:        "lbpending",
			Namespace:   "namespace1",
			Annotations: map[string]string{"foo": "bar", "bar": "foo"},
		},
		Status: core.ServiceStatus{
			LoadBalancer: core.LoadBalancerStatus{
				Ingress: []core.LoadBalancerIngress{},
			},
		},
	}
	k.client.CoreV1().Services(loadbalancer1.Namespace).Create(ctx, loadbalancer1, meta.CreateOptions{})
	k.client.CoreV1().Services(loadbalancer2.Namespace).Create(ctx, loadbalancer2, meta.CreateOptions{})
	k.client.CoreV1().Services(lbPending.Namespace).Create(ctx, lbPending, meta.CreateOptions{})
}

func runTests(t *testing.T, ctx context.Context, k *KubeLoadBalancer, cases []test.Case) {
	for i, tc := range cases {
		r := tc.Msg()
		w := dnstest.NewRecorder(&test.ResponseWriter{})

		_, err := k.ServeDNS(ctx, w, r)
		if err != tc.Error {
			t.Errorf("Test %d: %v", i, err)
			return
		}

		if w.Msg == nil {
			t.Errorf("Test %d: nil message", i)
			continue
		}
		if err := test.SortAndCheck(w.Msg, tc); err != nil {
			t.Errorf("Test %d: %v", i, err)
		}
	}
}
