# kubeloadbalancer

## Name

*kubeloadbalancer* - create records for Kubernetes LoadBalancer type Services.

## Description

*kubeloadbalancer* synthesizes A, AAAA records for LoadBalancer type Services' LoadBalance addresses.

By default, this plugin requires ...
* The [_kubeapi_ plugin](http://github.com/coredns/kubeapi) to make a connection
to the Kubernetes API.
* CoreDNS's Service Account has list/watch permission to the Serivces API.

This plugin can only be used once per Server Block.

## Syntax

```
kubeloadbalancer [ZONES...] {
    ttl TTL
    fallthrough [ZONES...]
}
```

* `ttl` allows you to set a custom TTL for responses. The default is 5 seconds.  The minimum TTL allowed is
  0 seconds, and the maximum is capped at 3600 seconds. Setting TTL to 0 will prevent records from being cached.
  All endpoint queries and headless service queries will result in an NXDOMAIN.
* `fallthrough` **[ZONES...]** If a query for a record in the zones for which the plugin is authoritative
  results in NXDOMAIN, normally that is what the response will be. However, if you specify this option,
  the query will instead be passed on down the plugin chain, which can include another plugin to handle
  the query. If **[ZONES...]** is omitted, then fallthrough happens for all zones for which the plugin
  is authoritative. If specific zones are listed (for example `in-addr.arpa` and `ip6.arpa`), then only
  queries for those zones will be subject to fallthrough.

## External Plugin

To use this plugin, compile CoreDNS with this plugin added to the `plugin.cfg`.
This plugin also requires the _kubeapi_ plugin, which should be added to the end of `plugin.cfg`.  
### Compile Notice:  
* Compile CoreDNS with the `Dockerfile` this project provided.
  *Modify CoreDNS project's .dockerignore file if use this Dockerfile*
* Add two plugins in the end of `plugin.cfg`
  ```
  kubeloadbalancer:github.com/chenwyi2/kubeloadbalancer
  kubeapi:github.com/coredns/kubeapi
  ```
  *Can't got get from Bitbucket which is not a valid go pacakge repository*

## Ready

This plugin reports that it is ready to the _ready_ plugin once it has received the complete list of Pods
from the Kubernetes API.

## Examples

Use Service's Status.loadbalancer.ingress.ip to answer ServiceName lookups in the zone `md-pv.saicmotortest.com.`.

```
md-pv.saicmotortest.com:53 {
  ...
  kubeapi {
    kubeconfig kube.config
  }
  kubeloadbalancer 
  ...
}
```