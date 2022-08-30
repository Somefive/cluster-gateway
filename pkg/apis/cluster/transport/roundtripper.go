package multicluster

import (
	"fmt"
	"net/http"
	"strings"

	"k8s.io/client-go/transport"
	"k8s.io/utils/pointer"

	"github.com/oam-dev/cluster-gateway/pkg/config"
)

var _ http.RoundTripper = &clusterGatewayRoundTripper{}

type clusterGatewayRoundTripper struct {
	delegate http.RoundTripper
	// falling back to the hosting cluster
	// this is required when the client does implicit api discovery
	// e.g. controller-runtime client
	fallback bool

	// redirect the original request to specified host
	// this can be used when client wants to dial cluster-gateway directly without connecting kube-apiserver
	host *string

	// filter the clusters that does not need proxy
	// this can be used to filter out clusters such as local cluster / empty cluster
	filter func(string) bool
}

// ClusterGatewayRoundTripperOption option for ClusterGatewayRoundTripper
type ClusterGatewayRoundTripperOption interface {
	ApplyToClusterGatewayRoundTripper(*clusterGatewayRoundTripper)
}

type clusterGatewayRoundTripperOption func(*clusterGatewayRoundTripper)

func (op clusterGatewayRoundTripperOption) ApplyToClusterGatewayRoundTripper(rt *clusterGatewayRoundTripper) {
	op(rt)
}

// ClusterGatewayRoundTripperWithHost specify the host to redirect while proxied
func ClusterGatewayRoundTripperWithHost(host string) ClusterGatewayRoundTripperOption {
	return clusterGatewayRoundTripperOption(func(rt *clusterGatewayRoundTripper) {
		rt.host = pointer.String(host)
	})
}

// ClusterGatewayRoundTripperWithFilter specify the filter to filter out clusters that does not need proxy
func ClusterGatewayRoundTripperWithFilter(filter func(string) bool) ClusterGatewayRoundTripperOption {
	return clusterGatewayRoundTripperOption(func(rt *clusterGatewayRoundTripper) {
		rt.filter = filter
	})
}

// NewClusterGatewayRoundTripperWrapper returns the wrapper for getting ClusterGatewayRoundTripper
func NewClusterGatewayRoundTripperWrapper(options ...ClusterGatewayRoundTripperOption) transport.WrapperFunc {
	return func(rt http.RoundTripper) http.RoundTripper {
		wrapped := NewClusterGatewayRoundTripper(rt).(*clusterGatewayRoundTripper)
		for _, op := range options {
			op.ApplyToClusterGatewayRoundTripper(wrapped)
		}
		return wrapped
	}
}

func NewClusterGatewayRoundTripper(delegate http.RoundTripper) http.RoundTripper {
	rt := &clusterGatewayRoundTripper{
		delegate: delegate,
		fallback: true,
	}
	return rt
}

func NewStrictClusterGatewayRoundTripper(delegate http.RoundTripper, fallback bool) http.RoundTripper {
	return &clusterGatewayRoundTripper{
		delegate: delegate,
		fallback: fallback,
	}
}

func (c *clusterGatewayRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	clusterName, exists := GetMultiClusterContext(request.Context())
	if !exists {
		if !c.fallback {
			return nil, fmt.Errorf("missing cluster name in the request context")
		}
		return c.delegate.RoundTrip(request)
	}
	if c.filter != nil && !c.filter(clusterName) {
		return c.delegate.RoundTrip(request)
	}
	request = request.Clone(request.Context())
	request.URL.Path = formatProxyURL(clusterName, request.URL.Path)
	if c.host != nil {
		request.Host = *c.host
	}
	return c.delegate.RoundTrip(request)
}

func formatProxyURL(clusterName, originalPath string) string {
	originalPath = strings.TrimPrefix(originalPath, "/")
	return strings.Join([]string{
		"/apis",
		config.MetaApiGroupName,
		config.MetaApiVersionName,
		"clustergateways",
		clusterName,
		"proxy",
		originalPath}, "/")
}
