package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E_PluginEndpointsExposeFrontendContract(t *testing.T) {
	ts := setupE2EServer(t)
	defer ts.Close()

	client := ts.Client()

	resp, err := client.Get(ts.URL + "/api/v1/plugins")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	resp, err = client.Get(ts.URL + "/api/v1/plugins/marketplace")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}
