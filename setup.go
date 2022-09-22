package kubeLoadBalancer

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/kubeapi"
)

const pluginName = "kubeloadbalancer"

var log = clog.NewWithPlugin(pluginName)

func init() { plugin.Register(pluginName, setup) }

func setup(c *caddy.Controller) error {
	k, err := parse(c)
	if err != nil {
		return plugin.Error(pluginName, err)
	}

	k.setWatch(context.Background())
	c.OnStartup(startWatch(k, dnsserver.GetConfig(c)))
	c.OnShutdown(stopWatch(k))

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		k.Next = next
		return k
	})

	return nil
}

func parse(c *caddy.Controller) (*KubeLoadBalancer, error) {
	var (
		kns *KubeLoadBalancer
		err error
	)

	i := 0
	for c.Next() {
		if i > 0 {
			return nil, plugin.ErrOnce
		}
		i++

		kns, err = parseStanza(c)
		if err != nil {
			return kns, err
		}
	}
	return kns, nil
}

func parseStanza(c *caddy.Controller) (*KubeLoadBalancer, error) {
	kps := New(plugin.OriginsFromArgsOrServerBlock(c.RemainingArgs(), c.ServerBlockKeys))
	for c.NextBlock() {
		switch c.Val() {
		case "fallthrough":
			kps.Fall.SetZonesFromArgs(c.RemainingArgs())
		case "ttl":
			args := c.RemainingArgs()
			if len(args) == 0 {
				return nil, c.ArgErr()
			}
			t, err := strconv.Atoi(args[0])
			if err != nil {
				return nil, err
			}
			if t < 0 || t > 3600 {
				return nil, c.Errf("ttl must be in range [0, 3600]: %d", t)
			}
			kps.ttl = uint32(t)
		default:
			return nil, c.Errf("unknown property '%s'", c.Val())
		}
	}

	return kps, nil
}

func (k *KubeLoadBalancer) setWatch(ctx context.Context) {
	// define Pod controller and reverse lookup indexer
	k.indexer, k.controller = cache.NewIndexerInformer(
		&cache.ListWatch{
			ListFunc: func(o meta.ListOptions) (runtime.Object, error) {
				return k.client.CoreV1().Services(core.NamespaceAll).List(ctx, o)
			},
			WatchFunc: func(o meta.ListOptions) (watch.Interface, error) {
				return k.client.CoreV1().Services(core.NamespaceAll).Watch(ctx, o)
			},
		},
		&core.Service{},
		0,
		cache.ResourceEventHandlerFuncs{},
		cache.Indexers{
			"name": func(obj interface{}) ([]string, error) {
				svc, ok := obj.(*core.Service)
				if !ok {
					return nil, errors.New("unexpected obj type")
				}
				return []string{svc.Name}, nil
			},
			"namewithns": func(obj interface{}) ([]string, error) {
				svc, ok := obj.(*core.Service)
				if !ok {
					return nil, errors.New("unexpected obj type")
				}
				namewithns := strings.Join([]string{svc.Namespace, svc.Name}, "/")
				return []string{namewithns}, nil
			},
		},
	)
}

func startWatch(k *KubeLoadBalancer, config *dnsserver.Config) func() error {
	return func() error {
		// retrieve client from kubeapi plugin
		var err error
		k.client, err = kubeapi.Client(config)
		if err != nil {
			return err
		}

		// start the informer
		go k.controller.Run(k.stopCh)
		return nil
	}
}

func stopWatch(k *KubeLoadBalancer) func() error {
	return func() error {
		k.stopLock.Lock()
		defer k.stopLock.Unlock()
		if !k.shutdown {
			close(k.stopCh)
			k.shutdown = true
			return nil
		}
		return fmt.Errorf("shutdown already in progress")
	}
}
