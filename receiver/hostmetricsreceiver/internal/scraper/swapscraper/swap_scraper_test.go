// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package swapscraper

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/collector/consumer/pdata"
	"go.opentelemetry.io/collector/receiver/hostmetricsreceiver/internal"
)

func TestScrape(t *testing.T) {
	type testCase struct {
		name              string
		bootTimeFunc      func() (uint64, error)
		expectedStartTime pdata.TimestampUnixNano
		initializationErr string
	}

	testCases := []testCase{
		{
			name: "Standard",
		},
		{
			name:              "Validate Start Time",
			bootTimeFunc:      func() (uint64, error) { return 100, nil },
			expectedStartTime: 100 * 1e9,
		},
		{
			name:              "Boot Time Error",
			bootTimeFunc:      func() (uint64, error) { return 0, errors.New("err1") },
			initializationErr: "err1",
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			scraper := newSwapScraper(context.Background(), &Config{})
			if test.bootTimeFunc != nil {
				scraper.bootTime = test.bootTimeFunc
			}

			err := scraper.Initialize(context.Background())
			if test.initializationErr != "" {
				assert.EqualError(t, err, test.initializationErr)
				return
			}
			require.NoError(t, err, "Failed to initialize swap scraper: %v", err)

			metrics, err := scraper.Scrape(context.Background())
			require.NoError(t, err)

			// expect 3 metrics (windows does not currently support page_faults metric)
			expectedMetrics := 3
			if runtime.GOOS == "windows" {
				expectedMetrics = 2
			}
			assert.Equal(t, expectedMetrics, metrics.Len())

			assertSwapUsageMetricValid(t, metrics.At(0))
			internal.AssertSameTimeStampForMetrics(t, metrics, 0, 1)

			assertPagingMetricValid(t, metrics.At(1), test.expectedStartTime)
			if runtime.GOOS != "windows" {
				assertPageFaultsMetricValid(t, metrics.At(2), test.expectedStartTime)
			}
			internal.AssertSameTimeStampForMetrics(t, metrics, 1, metrics.Len())
		})
	}
}

func assertSwapUsageMetricValid(t *testing.T, hostSwapUsageMetric pdata.Metric) {
	internal.AssertDescriptorEqual(t, swapUsageDescriptor, hostSwapUsageMetric)

	// it's valid for a system to have no swap space  / paging file, so if no data points were returned, do no validation
	if hostSwapUsageMetric.IntSum().DataPoints().Len() == 0 {
		return
	}

	// expect at least used, free & cached datapoint
	expectedDataPoints := 3
	// windows does not return a cached datapoint
	if runtime.GOOS == "windows" {
		expectedDataPoints = 2
	}

	assert.GreaterOrEqual(t, hostSwapUsageMetric.IntSum().DataPoints().Len(), expectedDataPoints)
	internal.AssertIntSumMetricLabelHasValue(t, hostSwapUsageMetric, 0, stateLabelName, usedLabelValue)
	internal.AssertIntSumMetricLabelHasValue(t, hostSwapUsageMetric, 1, stateLabelName, freeLabelValue)
	// on non-windows, also expect a cached state label
	if runtime.GOOS != "windows" {
		internal.AssertIntSumMetricLabelHasValue(t, hostSwapUsageMetric, 2, stateLabelName, cachedLabelValue)
	}
	// on windows, also expect the page file device name label
	if runtime.GOOS == "windows" {
		internal.AssertIntSumMetricLabelExists(t, hostSwapUsageMetric, 0, deviceLabelName)
		internal.AssertIntSumMetricLabelExists(t, hostSwapUsageMetric, 1, deviceLabelName)
	}
}

func assertPagingMetricValid(t *testing.T, pagingMetric pdata.Metric, startTime pdata.TimestampUnixNano) {
	internal.AssertDescriptorEqual(t, swapPagingDescriptor, pagingMetric)
	if startTime != 0 {
		internal.AssertIntSumMetricStartTimeEquals(t, pagingMetric, startTime)
	}

	// expect an in & out datapoint, for both major and minor paging types (windows does not currently support minor paging data)
	expectedDataPoints := 4
	if runtime.GOOS == "windows" {
		expectedDataPoints = 2
	}
	assert.Equal(t, expectedDataPoints, pagingMetric.IntSum().DataPoints().Len())

	internal.AssertIntSumMetricLabelHasValue(t, pagingMetric, 0, typeLabelName, majorTypeLabelValue)
	internal.AssertIntSumMetricLabelHasValue(t, pagingMetric, 0, directionLabelName, inDirectionLabelValue)
	internal.AssertIntSumMetricLabelHasValue(t, pagingMetric, 1, typeLabelName, majorTypeLabelValue)
	internal.AssertIntSumMetricLabelHasValue(t, pagingMetric, 1, directionLabelName, outDirectionLabelValue)
	if runtime.GOOS != "windows" {
		internal.AssertIntSumMetricLabelHasValue(t, pagingMetric, 2, typeLabelName, minorTypeLabelValue)
		internal.AssertIntSumMetricLabelHasValue(t, pagingMetric, 2, directionLabelName, inDirectionLabelValue)
		internal.AssertIntSumMetricLabelHasValue(t, pagingMetric, 3, typeLabelName, minorTypeLabelValue)
		internal.AssertIntSumMetricLabelHasValue(t, pagingMetric, 3, directionLabelName, outDirectionLabelValue)
	}
}

func assertPageFaultsMetricValid(t *testing.T, pageFaultsMetric pdata.Metric, startTime pdata.TimestampUnixNano) {
	internal.AssertDescriptorEqual(t, swapPageFaultsDescriptor, pageFaultsMetric)
	if startTime != 0 {
		internal.AssertIntSumMetricStartTimeEquals(t, pageFaultsMetric, startTime)
	}

	assert.Equal(t, 1, pageFaultsMetric.IntSum().DataPoints().Len())
	internal.AssertIntSumMetricLabelHasValue(t, pageFaultsMetric, 0, typeLabelName, minorTypeLabelValue)
}
