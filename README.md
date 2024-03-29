# ospfip

## Name

*ospfip* - resolve host names to tagged OpenStack Floatig IP's.


## Description

The *ospfip* plugin queries an OpenStack Floating IP API and resolves hostnames
found in predefined tags on Floating IP's.

Currently the plugin supports both A and AAAA (including wildcards) and PTR
records (excluding wildcards).

**Note:** This is intended for test/development environments. Use with care.

## How it authenticates

This plugin uses [gophercloud](https://github.com/gophercloud/gophercloud)
under the hood to work with the OpenStack API and relies on it for
authentication using either a `clouds.yaml` file or sourced `openrc` file.

To use the `clouds.yaml` file, place it at `~/.config/openstack/clouds.yaml`. To
use the openrc file, its values need to be set for the coredns process.

## How it works


The plugin queries the OpenStack API for Floating IP's with a `coredns:plugin:ospfip` tag.
Next the plugin extracts the to-be-resolved hostname from an additional `coredns:plugin:ospfip:<hostname>` tag.

Only the first encountered `coredns:plugin:ospfip:<hostname>` pair on
a Floating IP is taken into account.


## Syntax

~~~
ospfip [ZONES...] {
    ttl SECONDS
    refresh DURATION
}
~~~

* **ZONES** zones it should be authoritative for.
* `ttl` change the DNS TTL of the records generated. The default is 3600 seconds (1 hour).
* `refresh` the period between calls to the OpenStack Floating IP API to retrieve tagged
  Floating IP's. Valid formatting examples are  "300ms", "1.5h" or "2h45m". See
  Go's [time](https://pkg.go.dev/time). package.


## Examples

~~~ corefile
. {
    ospfip
}
~~~

In short, this:

~~~
$ openstack floating ip show <ID> -c tags -c floating_ip_address
+---------------------+---------------------------------------------------------------------------------+
| Field               | Value                                                                           |
+---------------------+---------------------------------------------------------------------------------+
| floating_ip_address | 10.0.0.1                                                                        |
| tags                | ['coredns:plugin:ospfip', 'coredns:plugin:ospfip:*.example.net']                |
+---------------------+---------------------------------------------------------------------------------+
~~~

resolves to (when running the coredns binary locally):

~~~
$ dig +noall +answer @0 q.example.net
q.example.net. 3600 IN   A       10.0.0.1
~~~

Limit the zones (origins) taken into account:

~~~ corefile
example.net. {
    ospfip
      refresh 2m
      ttl 5400
}
~~~
