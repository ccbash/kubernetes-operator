// SPDX-License-Identifier: BSD-3-Clause

package netbirdutil

import (
	"context"
	"errors"
	"fmt"
	"slices"

	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

// ErrZoneNotFound is returned by GetDNSZoneByName when no zone matches. It is a
// transient condition while a NetworkRouter is still creating its DNS zone, so
// callers can treat it as not-ready/requeue rather than a hard error.
var ErrZoneNotFound = errors.New("dns zone not found")

func GetDNSZoneByName(ctx context.Context, nbClient *netbird.Client, name string) (api.Zone, error) {
	resp, err := nbClient.DNSZones.ListZones(ctx)
	if err != nil {
		return api.Zone{}, err
	}
	zoneIdx := slices.IndexFunc(resp, func(zone api.Zone) bool {
		return zone.Name == name
	})
	if zoneIdx == -1 {
		return api.Zone{}, fmt.Errorf("%w: %s", ErrZoneNotFound, name)
	}
	return resp[zoneIdx], nil
}
