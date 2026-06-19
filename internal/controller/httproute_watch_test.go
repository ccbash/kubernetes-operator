// SPDX-License-Identifier: BSD-3-Clause

package controller

import (
	"context"
	"testing"

	"github.com/go-openapi/testify/v2/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	nbv1alpha1 "github.com/netbirdio/kubernetes-operator/api/v1alpha1"
)

func TestHTTPRoutesForNetworkResource(t *testing.T) {
	t.Parallel()

	netResource := &nbv1alpha1.NetworkResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				// The owning HTTPRoute (non-controller owner).
				{APIVersion: gatewayAPIGroup + "/v1", Kind: httpRouteKind, Name: "app-route"},
				// The backend Service (controller owner) — must be ignored.
				{APIVersion: "v1", Kind: "Service", Name: "app"},
				// An HTTPRoute-named kind from another group — must be ignored.
				{APIVersion: "example.com/v1", Kind: httpRouteKind, Name: "decoy"},
			},
		},
	}

	reqs := httpRoutesForNetworkResource(context.Background(), netResource)
	require.Len(t, reqs, 1)
	require.Equal(t, "app-route", reqs[0].Name)
	require.Equal(t, "default", reqs[0].Namespace)
}

func TestHTTPRoutesForNetworkResourceNoOwner(t *testing.T) {
	t.Parallel()

	netResource := &nbv1alpha1.NetworkResource{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default"},
	}
	require.Empty(t, httpRoutesForNetworkResource(context.Background(), netResource))
}

func TestNetworkResourceIDChanged(t *testing.T) {
	t.Parallel()

	withID := func(id string) *nbv1alpha1.NetworkResource {
		return &nbv1alpha1.NetworkResource{Status: nbv1alpha1.NetworkResourceStatus{ResourceID: id}}
	}

	// A change in resource ID (e.g. a routing-mode switch recreated it) triggers.
	require.True(t, networkResourceIDChanged.Update(event.UpdateEvent{
		ObjectOld: withID("res-1"),
		ObjectNew: withID("res-2"),
	}))

	// An unrelated status write (same ID) does not.
	require.False(t, networkResourceIDChanged.Update(event.UpdateEvent{
		ObjectOld: withID("res-1"),
		ObjectNew: withID("res-1"),
	}))

	// Creates trigger (new resource); deletes do not.
	require.True(t, networkResourceIDChanged.Create(event.CreateEvent{Object: withID("res-1")}))
	require.False(t, networkResourceIDChanged.Delete(event.DeleteEvent{Object: withID("res-1")}))
}
