/*
Copyright 2020 DigitalOcean
Copyright 2020 Flant

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ychelpers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	yclient "github.com/yandex-cloud/go-sdk"
)

type YandexInstanceIdentity struct {
	AvailabilityZone string `json:"availabilityZone"`
	InstanceID       string `json:"instanceId"`
	PrivateIP        string `json:"privateIp"`
	Region           string `json:"region"`
}

var (
	baseMetadataUrl                   = url.URL{Scheme: "http", Host: yclient.InstanceMetadataAddr}
	getInstanceIdentityTimeoutSeconds = 15
)

func GetInstanceIdentity() (YandexInstanceIdentity, error) {
	var metadataUrl = baseMetadataUrl
	metadataUrl.Path = "/latest/dynamic/instance-identity/document"

	client := http.Client{
		Timeout: time.Duration(getInstanceIdentityTimeoutSeconds) * time.Second,
	}

	resp, err := client.Get(metadataUrl.String())
	if err != nil {
		return YandexInstanceIdentity{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return YandexInstanceIdentity{}, fmt.Errorf("non-200 status %q returned on GET at URL %q", resp.StatusCode, metadataUrl.String())
	}

	identityBytes, err := io.ReadAll(resp.Body)
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

	hostname, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(hostname), nil
}
