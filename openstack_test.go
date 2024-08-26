package ospfip

import (
	"fmt"
	"net/http"
	"testing"

	fake "github.com/gophercloud/gophercloud/v2/openstack/networking/v2/common"
	th "github.com/gophercloud/gophercloud/v2/testhelper"
)

func TestList(t *testing.T) {

	const tag = "coredns:plugin:ospfip"

	tests := []struct {
		name       string
		tag        string
		want       int
		wantParams bool
		response   string
	}{
		{name: "list with tag query param", tag: tag, want: 1, wantParams: true, response: ListResponse(taggedFip)},
		{name: "list without tag query param", tag: "", want: 0, wantParams: false, response: ListResponse("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			th.SetupHTTP()
			defer th.TeardownHTTP()

			th.Mux.HandleFunc("/v2.0/floatingips", func(w http.ResponseWriter, r *http.Request) {
				th.TestMethod(t, r, "GET")
				th.TestHeader(t, r, "X-Auth-Token", fake.TokenID)

				w.Header().Add("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if tt.wantParams && len(r.URL.Query()) < 1 {
					t.Fatalf("expected query parameter, got %d", len(r.URL.Query()))
				}
				if !tt.wantParams && len(r.URL.Query()) > 0 {
					t.Fatalf("expected no query parameter, got %d", len(r.URL.Query()))
				}

				fmt.Fprintf(w, tt.response)
			})
			osc := &OpenStackClient{client: fake.ServiceClient()}
			got, err := osc.ListTaggedFips(tt.tag)
			if err != nil {
				t.Errorf("Failed to list tags: %s", err)
			}
			if len(got) != tt.want {
				t.Fatalf("expected to get %d fips, got: %s", tt.want, got)
			}
		})
	}
}
