package ospfip

import (
	"fmt"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/utils/openstack/clientconfig"
)

type OpenStackClient struct {
	client *gophercloud.ServiceClient
}

func NewOpenStackClient() (*OpenStackClient, error) {
	opts := new(clientconfig.ClientOpts)

	client, err := clientconfig.NewServiceClient("network", opts)
	if err != nil {
		return nil, err
	}
	return &OpenStackClient{client: client}, nil
}

func (osc *OpenStackClient) ListTaggedFips(tag string) ([]floatingips.FloatingIP, error) {

	listOpts := floatingips.ListOpts{
		Tags: tag,
	}

	allPages, err := floatingips.List(osc.client, listOpts).AllPages()
	if err != nil {
		return nil, fmt.Errorf("failed to list floating ips: %s", err)
	}

	allTaggedFIPs, err := floatingips.ExtractFloatingIPs(allPages)
	if err != nil {
		return nil, err
	}
	return allTaggedFIPs, nil
}
