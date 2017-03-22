package pmm

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/percona/pmm-client/test/fakeapi"
	protocfg "github.com/percona/pmm/proto/config"
	"github.com/stretchr/testify/assert"
)

func TestGetAgentID(t *testing.T) {
	// create tmpfile
	f, err := ioutil.TempFile("", "")
	assert.Nil(t, err)

	// remove it after test finishes
	defer os.Remove(f.Name())

	t.Run("correct file", func(t *testing.T) {
		config := &protocfg.Agent{
			UUID: "qwe123",
		}

		bytes, err := json.Marshal(config)
		assert.Nil(t, err)
		err = ioutil.WriteFile(f.Name(), bytes, 0600)
		assert.Nil(t, err)

		uuid, err := getAgentID(f.Name())
		assert.Nil(t, err)
		assert.Equal(t, config.UUID, uuid)
	})

	t.Run("incorrect file", func(t *testing.T) {
		config := &protocfg.Agent{}

		bytes, err := json.Marshal(config)
		assert.Nil(t, err)
		err = ioutil.WriteFile(f.Name(), bytes, 0600)
		assert.Nil(t, err)

		uuid, err := getAgentID(f.Name())
		assert.Error(t, err)
		assert.Equal(t, config.UUID, uuid)
	})
}

func TestAdmin_StartStopQAN(t *testing.T) {
	t.Parallel()

	agentID := "123"

	// create fakeapi
	api := fakeapi.New()
	defer api.Close()
	api.AppendQanAPIAgents(agentID)

	u, _ := url.Parse(api.URL())

	// create qan config
	qanConfig := map[string]interface{}{
		"UUID":           "xyz",
		"Interval":       60,
		"ExampleQueries": true,
	}

	// create pmm-admin instance
	admin := &Admin{}
	insecureFlag := true
	timeout := 1 * time.Second
	admin.qanAPI = NewAPI(insecureFlag, timeout)

	// point pmm-admin to fake http api
	admin.serverURL = u.Host
	scheme := "http"
	authStr := ""
	host := u.Host // u.Host delivers host:port
	admin.serverURL = fmt.Sprintf("%s://%s%s", scheme, authStr, host)

	t.Run("startQAN", func(t *testing.T) {
		err := admin.startQAN(agentID, qanConfig)
		assert.Nil(t, err)
	})

	t.Run("stopQAN", func(t *testing.T) {
		err := admin.stopQAN(agentID, "qwe")
		assert.Nil(t, err)
	})
}
