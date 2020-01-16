package ychelpers

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	yclient "github.com/yandex-cloud/go-sdk"
)

type YandexInstanceIdentity struct {
	AvailabilityZone string `json:"availabilityZone"`
	InstanceID       string `json:"instanceId"`
	PrivateIP        string `json:"privateIp"`
	Region           string `json:"region"`
}

var baseMetadataUrl = url.URL{Scheme: "http", Host: yclient.InstanceMetadataAddr}

func GetInstanceIdentity() (YandexInstanceIdentity, error) {
	var metadataUrl = baseMetadataUrl
	metadataUrl.Path = "/latest/dynamic/instance-identity/document"
	resp, err := http.Get(metadataUrl.String())
	if err != nil {
		return YandexInstanceIdentity{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return YandexInstanceIdentity{}, fmt.Errorf("non-200 status %q returned on GET at URL %q", resp.StatusCode, metadataUrl.String())
	}

	identityBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return YandexInstanceIdentity{}, err
	}

	var identity YandexInstanceIdentity
	err = json.Unmarshal(identityBytes, &identity)
	if err != nil {
		return YandexInstanceIdentity{}, err
	}

	return identity, nil
}

func GetHostname() (string, error) {
	var metadataUrl = baseMetadataUrl
	metadataUrl.Path = "/latest/meta-data/hostname"
	resp, err := http.Get(metadataUrl.String())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("non-200 status %q returned on GET at URL %q", resp.StatusCode, metadataUrl.String())
	}

	hostname, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(hostname), nil
}
