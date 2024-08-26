package ospfip

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/config"
	"github.com/gophercloud/gophercloud/v2/openstack/config/clouds"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/floatingips"
)

type OpenStackClient struct {
	client *gophercloud.ServiceClient
}

func NewOpenStackClient() (*OpenStackClient, error) {
	ctx := context.Background()

	authOpts, endpointOptions, tlsConfig, err := clouds.Parse()
	if err != nil {
		panic(err)
	}

	authOpts.AllowReauth = true
	providerClient, err := config.NewProviderClient(ctx, authOpts, config.WithTLSConfig(tlsConfig))
	if err != nil {
		panic(err)
	}

	client, err := openstack.NewNetworkV2(providerClient, endpointOptions)
	if err != nil {
		panic(err)
	}
	return &OpenStackClient{client: client}, nil
}

func (osc *OpenStackClient) ListTaggedFips(tag string) ([]floatingips.FloatingIP, error) {

	listOpts := floatingips.ListOpts{
		Tags: tag,
	}

	allPages, err := floatingips.List(osc.client, listOpts).AllPages(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("failed to list floating ips: %s", err)
	}

	allTaggedFIPs, err := floatingips.ExtractFloatingIPs(allPages)
	if err != nil {
		return nil, err
	}
	return allTaggedFIPs, nil
}
