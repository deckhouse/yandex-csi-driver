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

package driver

import (
	"context"
	"fmt"
	"time"

	"github.com/yandex-cloud/go-genproto/yandex/cloud/endpoint"
	ycsdk "github.com/yandex-cloud/go-sdk"

	"golang.org/x/sync/errgroup"
)

// HealthCheck is the interface that must be implemented to be compatible with
// `HealthChecker`.
type HealthCheck interface {
	Name() string
	Check(context.Context) error
}

// HealthChecker helps with writing multi component health checkers.
type HealthChecker struct {
	checks []HealthCheck
}

// NewHealthChecker configures a new health checker with the passed in checks.
func NewHealthChecker(checks ...HealthCheck) *HealthChecker {
	return &HealthChecker{
		checks: checks,
	}
}

// Check runs all configured health checks and return an error if any of the
// checks fail.
func (c *HealthChecker) Check(ctx context.Context) error {
	var eg errgroup.Group

	for _, check := range c.checks {
		check := check
		eg.Go(func() error {
			return check.Check(ctx)
		})
	}

	return eg.Wait()
}

var yandexHealthTimeout = 15 * time.Second

type yandexHealthChecker struct {
	sdk *ycsdk.SDK
}

func (c *yandexHealthChecker) Name() string {
	return "yandex"
}

func (c *yandexHealthChecker) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, yandexHealthTimeout)
	defer cancel()
	_, err := c.sdk.ApiEndpoint().ApiEndpoint().List(ctx, &endpoint.ListApiEndpointsRequest{})
	if err != nil {
		return fmt.Errorf("checking yandex health: %w", err)
	}
	return nil
}
