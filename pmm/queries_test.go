package pmm

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"testing"

	protocfg "github.com/percona/pmm/proto/config"
	"github.com/stretchr/testify/assert"
)

func TestGetAgentID(t *testing.T) {
	// create tmpfile
	f, err := ioutil.TempFile("", "")
	assert.Nil(t, err)

	// remove it after test finishes
	defer os.Remove(f.Name())

	// test correct file
	{
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
	}

	// test incorrect file
	{
		config := &protocfg.Agent{}

		bytes, err := json.Marshal(config)
		assert.Nil(t, err)
		err = ioutil.WriteFile(f.Name(), bytes, 0600)
		assert.Nil(t, err)

		uuid, err := getAgentID(f.Name())
		assert.Error(t, err)
		assert.Equal(t, config.UUID, uuid)
	}
}
