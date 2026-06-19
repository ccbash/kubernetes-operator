// SPDX-License-Identifier: BSD-3-Clause

package netbirdutil

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/go-openapi/testify/v2/require"

	netbird "github.com/netbirdio/netbird/shared/management/client/rest"
)

func TestIsConflict(t *testing.T) {
	t.Parallel()

	// 400, 409 and 412 are the "still in use" responses callers back off on.
	// 412 (Precondition Failed) is NetBird's "resource is in use by proxy".
	require.True(t, IsConflict(&netbird.APIError{StatusCode: http.StatusBadRequest}))
	require.True(t, IsConflict(&netbird.APIError{StatusCode: http.StatusConflict}))
	require.True(t, IsConflict(&netbird.APIError{StatusCode: http.StatusPreconditionFailed}))

	// A wrapped API error is still recognised.
	require.True(t, IsConflict(fmt.Errorf("delete group: %w", &netbird.APIError{StatusCode: http.StatusConflict})))
	require.True(t, IsConflict(fmt.Errorf("delete resource: %w", &netbird.APIError{
		StatusCode: http.StatusPreconditionFailed,
		Message:    "resource d8pdh105n19c73a0u7lg is in use by proxy d8pdhg05n19c73a0u8r0",
	})))

	// Other statuses, non-API errors and nil are not conflicts.
	require.False(t, IsConflict(&netbird.APIError{StatusCode: http.StatusNotFound}))
	require.False(t, IsConflict(&netbird.APIError{StatusCode: http.StatusInternalServerError}))
	require.False(t, IsConflict(errors.New("boom")))
	require.False(t, IsConflict(nil))
}
