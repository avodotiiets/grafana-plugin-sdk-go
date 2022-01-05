package sdata_test

import (
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/grafana/grafana-plugin-sdk-go/experimental/sdata"
	"github.com/stretchr/testify/require"
)

func TestPlayGround(t *testing.T) {
	var mfs sdata.MultiFrameSeries
	mfs.AddMetric("os.cpu", data.Labels{"host": "a"}, []time.Time{time.Unix(1234567890, 0)}, []float64{3})
}

func emptyFrameWithManySeriesMD() *data.Frame {
	return data.NewFrame("").SetMeta(&data.FrameMeta{Type: data.FrameTypeTimeSeriesMany})
}

func addFields(frame *data.Frame, fields ...*data.Field) *data.Frame {
	frame.Fields = append(frame.Fields, fields...)
	return frame
}

func TestMultiFrameSeriesValidiate_WithFrames_InvalidCases(t *testing.T) {
	tests := []struct {
		name          string
		mfs           *sdata.MultiFrameSeries
		empty         bool
		errCount      int
		errorsContain []string
	}{
		{
			name: "frame must have type indicator",
			mfs: &sdata.MultiFrameSeries{
				data.NewFrame(""),
			},
			errCount:      1,
			errorsContain: []string{"missing type indicator"},
		},
		{
			name: "frame with only value field is not valid, missing time field",
			mfs: &sdata.MultiFrameSeries{
				addFields(emptyFrameWithManySeriesMD(),
					data.NewField("", nil, []float64{})),
			},
			errCount:      1,
			errorsContain: []string{"must have exactly 1 time field"},
		},
		{
			name: "frame with only a time field and no value is not valid",
			mfs: &sdata.MultiFrameSeries{
				addFields(emptyFrameWithManySeriesMD(),
					data.NewField("", nil, []time.Time{})),
			},
			errCount:      1,
			errorsContain: []string{"must have exactly 1 value field"},
		},
		{
			name: "fields must be of the same length",
			mfs: &sdata.MultiFrameSeries{
				addFields(emptyFrameWithManySeriesMD(),
					data.NewField("", nil, []float64{1, 2}),
					data.NewField("", nil, []time.Time{time.UnixMilli(1)})),
			},
			errCount:      1,
			errorsContain: []string{"mismatched field lengths"},
		},
		{
			name: "frame with unsorted time is not valid",
			mfs: &sdata.MultiFrameSeries{
				addFields(emptyFrameWithManySeriesMD(),
					data.NewField("", nil, []float64{1, 2}),
					data.NewField("", nil, []time.Time{time.UnixMilli(2), time.UnixMilli(1)})),
			},
			errCount:      1,
			errorsContain: []string{"unsorted time"},
		},
		{
			name: "duplicate metrics as identified by name + labes are invalid",
			mfs: &sdata.MultiFrameSeries{
				addFields(emptyFrameWithManySeriesMD(),
					data.NewField("os.cpu", data.Labels{"host": "a", "iface": "eth0"}, []float64{1, 2}),
					data.NewField("", nil, []time.Time{time.UnixMilli(1), time.UnixMilli(2)})),
				addFields(emptyFrameWithManySeriesMD(),
					data.NewField("os.cpu", data.Labels{"iface": "eth0", "host": "a"}, []float64{1, 2}),
					data.NewField("", nil, []time.Time{time.UnixMilli(1), time.UnixMilli(2)})),
			},
			errCount:      1,
			errorsContain: []string{"duplicate metrics found"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			empty, errors := tt.mfs.Validate()
			for _, errSubStr := range tt.errorsContain {
				foundErr := false
				for _, err := range errors {
					if strings.Contains(err.Error(), errSubStr) {
						foundErr = true
						break
					}
				}
				require.True(t, foundErr, "expected error substring %q not found, errors: %v", errSubStr, errors)
			}
			require.Equal(t, tt.errCount, len(errors), "expected %v validation errors, errors: %v", tt.errCount, errors)
			require.Equal(t, tt.empty, empty, "expected valid to be %v", tt.empty)
		})
	}
}

func TestMultiFrameSeriesValidiate_WithFrames_ValidCases(t *testing.T) {
	tests := []struct {
		name  string
		mfs   *sdata.MultiFrameSeries
		empty bool
	}{
		{
			name:  "nil or empty set is valid and empty",
			empty: true,
		},
		{
			name: "frame with no fields is valid, and does not mean set is empty",
			mfs: &sdata.MultiFrameSeries{
				emptyFrameWithManySeriesMD(),
			},
		},
		{
			name: "frame with unsorted time is not valid",
			mfs: &sdata.MultiFrameSeries{
				addFields(emptyFrameWithManySeriesMD(),
					data.NewField("", nil, []bool{true, false}),
					data.NewField("", nil, []time.Time{time.UnixMilli(1), time.UnixMilli(2)})),
			},
		},
		{
			name: "there can be extraneous string fields (but they have no specific platform-wide meaning)",
			mfs: &sdata.MultiFrameSeries{
				addFields(emptyFrameWithManySeriesMD(),
					data.NewField("", nil, []string{"fair", "enough?"}),
					data.NewField("", nil, []int64{2, 3}),
					data.NewField("", nil, []time.Time{time.UnixMilli(1), time.UnixMilli(2)})),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			empty, errors := tt.mfs.Validate()
			require.Zero(t, errors)
			require.Equal(t, tt.empty, empty, "expected valid to be %v", tt.empty)
		})
	}
}