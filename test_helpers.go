package ospfip

import (
	"fmt"
	"strings"
)

const taggedFip = `
{
        "id": "49426401-21ef-4314-a5ca-05423f4405ad",
        "tenant_id": "eac7ae24f17790eec436bd46c71834d8",
        "floating_ip_address": "192.0.0.3",
        "fixed_ip_address": "192.168.0.3",
        "status": "DOWN",
        "tags": [
          "coredns:plugin:ospfip",
          "coredns:plugin:ospfip:api.mycluster.example.net"
        ]
}`

const taggedWildcardFip = `
{
        "id": "59de6fdb-997e-4034-871d-4face7e5a259",
        "tenant_id": "7a2fbd446e1837790eeeac36c714c4d8",
        "floating_ip_address": "192.0.0.4",
        "fixed_ip_address": "192.168.0.4",
        "status": "DOWN",
        "tags": [
          "coredns:plugin:ospfip",
          "coredns:plugin:ospfip:*.mycluster.example.net"
        ]
}`

const untaggedFip = `
{
        "id": "c8158015-8904-4cd0-8932-e0a342b39c65",
        "tenant_id": "ee64e478d35cbc7eaf1229be17a",
        "floating_ip_address": "192.0.0.5",
        "fixed_ip_address": "192.168.0.5",
        "status": "DOWN",
        "tags": []
}`

var ListResponse = func(fipResp ...string) string {
	return fmt.Sprintf(`
        {
            "floatingips": [
        %s
            ]
        }
        `, strings.Join(fipResp, ", "))
}
