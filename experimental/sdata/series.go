package sdata

import (
	"fmt"
	"sort"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Less sure about this interface.
// Also, probably would need to make a type with methods that wrap the interface so methods can be added
// without breaking changes - if we even want an interface like this
// But for now helps illustrate at least
type TimeSeriesCollectionWriter interface {
	AddMetric(metricName string, l data.Labels, t []time.Time, values interface{}) error
	SetMetricMD(metricName string, l data.Labels, fc data.FieldConfig)
	// AddField Possible TODO - If we accept certain extra information to be valid but outside the series
}

type TimeSeriesCollection interface {
	TimeSeriesCollectionReader
	TimeSeriesCollectionWriter
}

type TimeSeriesCollectionReader interface {
	Validate() (isEmpty bool, errors []error)
	GetMetricRefs() []TimeSeriesMetricRef
}

func ValidValueFields() []data.FieldType {
	return append(data.NumericFieldTypes(), []data.FieldType{data.FieldTypeBool, data.FieldTypeNullableBool}...)
}

// or perhaps a container struct with non-exported fields (for indicies and such) and the Frames exported.
type MultiFrameSeries []*data.Frame

// values must be a numeric slice such as []int64, []float64, []*float64, etc or []bool / []*bool.
func (mfs *MultiFrameSeries) AddMetric(metricName string, l data.Labels, t []time.Time, values interface{}) error {
	var err error

	if !data.ValidFieldType(values) {
		return fmt.Errorf("type %T is not a valid data frame field type", values)
	}

	valueField := data.NewField(metricName, l, values) // note
	timeField := data.NewField("time", nil, t)

	if valueField.Len() != timeField.Len() {
		// return error since creating the frame will eventually fail to marshal due to the
		// arrow constraint that fields must be of the same length.
		// Alternatively we could pad, but this seems like it would be a programing error more than
		// a data error to me.
		return fmt.Errorf("invalid series, time and value must be of the same length")
	}

	valueFieldType := valueField.Type()
	if !valueFieldType.Numeric() && valueFieldType != data.FieldTypeBool && valueFieldType != data.FieldTypeNullableBool {
		err = fmt.Errorf("value type %s is not valid time series value type", valueFieldType)
	}

	frame := data.NewFrame("", timeField, valueField)
	frame.SetMeta(&data.FrameMeta{Type: data.FrameTypeTimeSeriesMany}) // I think "Multi" is better than "Many"
	*mfs = append(*mfs, frame)
	return err
}

func (mfs *MultiFrameSeries) SetMetricMD(metricName string, l data.Labels, fc data.FieldConfig) {
}

// Validates data conforms to schema, don't think it will be called normally in the course of running a plugin, but needs to exist.
// Currently this is strict in the sense that consumers must support all valid instances. Consumers may support invalid instances
// depending on the circumstances.
func (mfs *MultiFrameSeries) Validate() (isEmpty bool, errors []error) {
	if mfs == nil || len(*mfs) == 0 {
		// Unless we create a container (and expose it in our responses) that can hold the type(s) for the frames it contains,
		// anything empty probably needs be considered "valid" for the type. Else we have a requirement to create at least one frame (eww).
		return true, nil
	}

	metricIndex := make(map[[2]string]struct{})

	for fIdx, frame := range *mfs {
		if frame.Meta == nil || frame.Meta.Type != data.FrameTypeTimeSeriesMany {
			errors = append(errors, fmt.Errorf("frame %v is missing type indicator in frame metadata", fIdx))
		}

		if len(frame.Fields) == 0 {
			// an individual frame with no fields is an empty series is valid.
			continue
		}

		if _, err := frame.RowLen(); err != nil {
			errors = append(errors, fmt.Errorf("frame %v has mismatched field lengths: %w", fIdx, err))
		}

		timeFields := frame.TypeIndices(data.FieldTypeTime)

		// Must have []time.Time field (no nullable time)
		if len(timeFields) != 1 {
			errors = append(errors, fmt.Errorf("frame %v must have exactly 1 time field but has %v", fIdx, len(timeFields)))
		} else {
			// Validate time Field is sorted in ascending (oldest to newest) order
			timeField := frame.Fields[timeFields[0]]
			if timeField.Len() > 1 {
				for tIdx := 1; tIdx < timeField.Len(); tIdx++ {
					prevTime := timeField.At(tIdx - 1).(time.Time)
					curTime := timeField.At(tIdx).(time.Time)
					if curTime.Before(prevTime) {
						errors = append(errors, fmt.Errorf("frame %v has an unsorted time field", fIdx))
						break
					}
				}
			}

			valueFields := frame.TypeIndices(ValidValueFields()...)

			if len(valueFields) != 1 {
				errors = append(errors, fmt.Errorf("frame %v must have exactly 1 value field but has %v", fIdx, len(valueFields)))
			} else {
				vField := frame.Fields[valueFields[0]]

				metricKey := [2]string{vField.Name, vField.Labels.String()}
				if _, ok := metricIndex[metricKey]; ok {
					errors = append(errors, fmt.Errorf("duplicate metrics found for metric name %q and labels %q", vField.Name, vField.Labels))
				} else {
					metricIndex[metricKey] = struct{}{}
				}
			}
		}
	}

	return false, errors
}

// I am not sure about this but want to get the idea down
type TimeSeriesMetricRef struct {
	ValueField *data.Field
	TimeField  *data.Field
}

func (m TimeSeriesMetricRef) GetName() string {
	if m.ValueField != nil {
		return m.ValueField.Name
	}
	return ""
}

func (m TimeSeriesMetricRef) GetLabels() data.Labels {
	if m.ValueField != nil {
		return m.ValueField.Labels
	}
	return nil
}

func (mfs *MultiFrameSeries) GetMetricRefs() []TimeSeriesMetricRef {
	refs := []TimeSeriesMetricRef{}
	if mfs == nil || len(*mfs) == 0 {
		return refs
	}
	for _, frame := range *mfs {
		m := TimeSeriesMetricRef{}
		if len(frame.Fields) == 0 {
			refs = append(refs, m)
		}
		timeFields := frame.TypeIndices(data.FieldTypeTime)
		if len(timeFields) == 1 {
			m.TimeField = frame.Fields[timeFields[0]]
		}

		valueFields := frame.TypeIndices(ValidValueFields()...)
		if len(timeFields) == 1 {
			m.ValueField = frame.Fields[valueFields[0]]
		}
		refs = append(refs, m)
	}
	return refs
}

// need to think about pointers here and elsewhere
type WideFrameSeries struct {
	*data.Frame
}

func (wf *WideFrameSeries) AddMetric(metricName string, l data.Labels, t []time.Time, values interface{}) error {
	if !data.ValidFieldType(values) {
		return fmt.Errorf("type %T is not a valid data frame field type", values)
	}

	tFieldIndex := -1
	var timeIndicies []int

	if wf.Frame != nil {
		timeIndicies = wf.Frame.TypeIndices(data.FieldTypeTime)
	}

	if len(timeIndicies) != 0 {
		tFieldIndex = timeIndicies[0]
	}

	if t == nil && tFieldIndex == -1 {
		return fmt.Errorf("must provide time field when adding first metric")
	}

	if t != nil && tFieldIndex > -1 {
		return fmt.Errorf("time field must only be provided once")
	}

	valueField := data.NewField(metricName, l, values)
	var timeField *data.Field
	if t != nil {
		timeField = data.NewField("time", nil, t)
	} else {
		timeField = wf.Frame.Fields[tFieldIndex]
	}

	if valueField.Len() != timeField.Len() {
		return fmt.Errorf("value field length must match time field length, but gots length %v for time and %v for values",
			timeField.Len(), valueField.Len())
	}

	if t != nil {
		wf.Frame = data.NewFrame("", timeField, valueField)
		wf.Frame.SetMeta(&data.FrameMeta{Type: data.FrameTypeTimeSeriesWide})
	} else {
		wf.Fields = append(wf.Fields, valueField)
	}

	return nil
}

func (wf *WideFrameSeries) GetMetricRefs() []TimeSeriesMetricRef {
	refs := []TimeSeriesMetricRef{}
	if wf == nil || wf.Frame == nil {
		return refs
	}
	timeFields := wf.TypeIndices(data.FieldTypeTime)
	var timeField *data.Field
	if len(timeFields) == 1 {
		timeField = wf.Fields[timeFields[0]]
	}

	valueFieldIndicies := wf.TypeIndices(ValidValueFields()...)
	for _, fieldIdx := range valueFieldIndicies {
		refs = append(refs, TimeSeriesMetricRef{
			TimeField:  timeField,
			ValueField: wf.Fields[fieldIdx],
		})
	}
	return refs
}

func (wf *WideFrameSeries) SetMetricMD(metricName string, l data.Labels, fc data.FieldConfig) {
	panic("not implemented")
}

func (wf *WideFrameSeries) Validate() (isEmpty bool, err []error) {
	return false, nil
}

// LongSeries is only a TimeSeriesCollectionReader (not a Writer) .. for now.
// for now because, maybe we do want methods for creation, but they would hold
// the orginal table format, so really it would be validation and adding the meta
// property
type LongSeries struct {
	*data.Frame
	BoolAsMetric bool
}

func (ls *LongSeries) Validate() (isEmpty bool, errors []error) {
	panic("not implemented")
}

func (ls *LongSeries) GetMetricRefs() []TimeSeriesMetricRef {
	if ls == nil || ls.Frame == nil || ls.Fields == nil {
		return []TimeSeriesMetricRef{}
	}
	// metricName/labels -> SeriesRef
	mm := make(map[string]map[string]TimeSeriesMetricRef)

	refs := []TimeSeriesMetricRef{}
	appendToMetric := func(metricName string, l data.Labels, t time.Time, value interface{}) {
		if mm[metricName] == nil {
			mm[metricName] = make(map[string]TimeSeriesMetricRef)
		}

		lbStr := l.String()
		if ref, ok := mm[metricName][lbStr]; !ok {
			// TODO could carry time field name
			ref.TimeField = data.NewField("time", nil, []time.Time{t})

			vt := data.FieldTypeFor(value)
			ref.ValueField = data.NewFieldFromFieldType(vt, 1)
			ref.ValueField.Set(0, value)
			ref.ValueField.Name = metricName
			ref.ValueField.Labels = l

			mm[metricName][lbStr] = ref
			refs = append(refs, ref)
		} else {
			ref.TimeField.Append(t)
			ref.ValueField.Append(value)
		}
	}

	timeFields := ls.TypeIndices(data.FieldTypeTime)
	var timeField *data.Field
	if len(timeFields) > 0 {
		timeField = ls.Fields[timeFields[0]]
	} else {
		return []TimeSeriesMetricRef{}
	}

	valueFieldIndicies := ls.TypeIndices(ValidValueFields()...) // TODO switch on bool type option
	if len(valueFieldIndicies) == 0 {
		return []TimeSeriesMetricRef{}
	}

	factorFieldIndicies := ls.TypeIndices(data.FieldTypeString, data.FieldTypeNullableString)

	for rowIdx := 0; rowIdx < ls.Rows(); rowIdx++ {
		l := data.Labels{}
		for _, strFieldIdx := range factorFieldIndicies {
			cv, _ := ls.ConcreteAt(strFieldIdx, rowIdx)
			l[ls.Fields[strFieldIdx].Name] = cv.(string)
		}
		for _, vFieldIdx := range valueFieldIndicies {
			valueField := ls.Fields[vFieldIdx]
			appendToMetric(valueField.Name, l, timeField.At(rowIdx).(time.Time), valueField.At(rowIdx))
		}
	}
	sortRefs(refs)
	return refs
}

func sortRefs(refs []TimeSeriesMetricRef) {
	sort.SliceStable(refs, func(i, j int) bool {
		iRef := refs[i]
		jRef := refs[j]

		if iRef.GetName() < jRef.GetName() {
			return true
		}
		if iRef.GetName() > jRef.GetName() {
			return false
		}

		// If here Names are equal, next sort based on if there are labels.

		if iRef.GetLabels() == nil && jRef.GetLabels() == nil {
			return true // no labels first
		}
		if iRef.GetLabels() == nil && jRef.GetLabels() != nil {
			return true
		}
		if iRef.GetLabels() != nil && jRef.GetLabels() == nil {
			return false
		}

		return iRef.GetLabels().String() < jRef.GetLabels().String()
	})
}
