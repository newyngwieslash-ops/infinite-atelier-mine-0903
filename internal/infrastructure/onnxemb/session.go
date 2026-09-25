//go:build cgo

package onnxemb

import (
	"strings"

	ort "github.com/yalue/onnxruntime_go"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"runtime"
)

// session.go opens an ONNX session and reads what the model DECLARES.
//
// # Why the shapes are read rather than configured
//
// A BERT-family encoder declares `input_ids [batch, sequence]`, `attention_mask` and `token_type_ids`
// with the same shapes, and `last_hidden_state [batch, sequence, dimensions]`. Reading them from the
// file is what turns three silent-failure modes into refusals:
//
//   - a model whose sequence limit is not what a caller assumed,
//   - a model whose embedding width is not what the index was built for,
//   - a model that is not an encoder at all, because its inputs are named something else.
//
// Each of those, if assumed instead of checked, produces vectors of the wrong shape or a run that
// fails at the first inference with a message about tensors rather than about the model.
//
// # Why the session is built for a FIXED shape
//
// The tensors are allocated once and reused, so every text is padded to the same sequence length.
// The alternative — a new session per text length — would rebuild the model's execution plan on
// every call, which for a rebuild over a thousand memories is the difference between seconds and
// minutes. The cost is that padding is real work, and the attention mask is what makes it harmless.

// modelInputs is one open session and the five tensors it reads and writes.
type modelInputs struct {
	session *ort.AdvancedSession
	ids     *ort.Tensor[int64]
	mask    *ort.Tensor[int64]
	types   *ort.Tensor[int64]
	out     *ort.Tensor[float32]
}

// sessionShapes is what the model declared.
type sessionShapes struct {
	sequence   int
	dimensions int
}

// openSession loads a model and reports its shapes.
//
// The three input names are required by name rather than accepted in whatever order the file lists
// them: the pooler's correctness depends on the MASK being the mask, and a session built with the
// arguments in file order would silently compute attention over padding for a model that declares
// them alphabetically.
func openSession(modelPath string) (*modelInputs, sessionShapes, error) {
	inputs, outputs, err := ort.GetInputOutputInfo(modelPath)
	if err != nil {
		return nil, sessionShapes{}, apperror.New("ONNX_MODEL_UNREADABLE", "configuration", false,
			"The embedding model could not be read: "+err.Error(), err)
	}
	inputNames := map[string]bool{}
	for _, input := range inputs {
		inputNames[input.Name] = true
	}
	for _, required := range []string{"input_ids", "attention_mask", "token_type_ids"} {
		if !inputNames[required] {
			return nil, sessionShapes{}, apperror.New("ONNX_MODEL_NOT_AN_ENCODER", "configuration", false,
				"That model does not take the inputs an embedding encoder needs.", nil)
		}
	}
	if len(outputs) != 1 {
		return nil, sessionShapes{}, apperror.New("ONNX_MODEL_OUTPUT_SHAPE", "configuration", false,
			"That model does not return exactly one output, so its embedding cannot be read.", nil)
	}
	// The LAST axis is the embedding width and it must be a positive constant: a symbolic one means
	// the width depends on the input, which is not an embedding this index can store.
	dimensions := lastStaticDimension(outputs[0].Dimensions)
	if dimensions <= 0 {
		return nil, sessionShapes{}, apperror.New("ONNX_MODEL_OUTPUT_SHAPE", "configuration", false,
			"That model's embedding width is not fixed, so it cannot be indexed.", nil)
	}
	// The SEQUENCE length is this build's own choice within what the model allows: the declared
	// second axis is often symbolic (`-1`), and a fixed length is what lets the tensors be reused.
	sequence := DefaultMaxTokens
	if declared := secondStaticDimension(outputs[0].Dimensions); declared > 0 && declared < sequence {
		sequence = declared
	}

	ids, err := ort.NewTensor(ort.NewShape(1, int64(sequence)), make([]int64, sequence))
	if err != nil {
		return nil, sessionShapes{}, apperror.New("ONNX_TENSOR_FAILED", "unavailable", false,
			"The embedding model's input could not be prepared.", err)
	}
	mask, err := ort.NewTensor(ort.NewShape(1, int64(sequence)), make([]int64, sequence))
	if err != nil {
		ids.Destroy()
		return nil, sessionShapes{}, apperror.New("ONNX_TENSOR_FAILED", "unavailable", false,
			"The embedding model's input could not be prepared.", err)
	}
	types, err := ort.NewTensor(ort.NewShape(1, int64(sequence)), make([]int64, sequence))
	if err != nil {
		ids.Destroy()
		mask.Destroy()
		return nil, sessionShapes{}, apperror.New("ONNX_TENSOR_FAILED", "unavailable", false,
			"The embedding model's input could not be prepared.", err)
	}
	out, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(sequence), int64(dimensions)))
	if err != nil {
		ids.Destroy()
		mask.Destroy()
		types.Destroy()
		return nil, sessionShapes{}, apperror.New("ONNX_TENSOR_FAILED", "unavailable", false,
			"The embedding model's output could not be prepared.", err)
	}
	session, err := ort.NewAdvancedSession(modelPath,
		// The order the tensors are passed in, which is why the names are stated here rather than
		// taken from the file.
		[]string{"input_ids", "attention_mask", "token_type_ids"},
		[]string{outputs[0].Name},
		[]ort.Value{ids, mask, types},
		[]ort.Value{out},
		nil)
	if err != nil {
		ids.Destroy()
		mask.Destroy()
		types.Destroy()
		out.Destroy()
		return nil, sessionShapes{}, apperror.New("ONNX_SESSION_FAILED", "unavailable", false,
			"The embedding model could not be opened.", err)
	}
	return &modelInputs{session: session, ids: ids, mask: mask, types: types, out: out},
		sessionShapes{sequence: sequence, dimensions: dimensions}, nil
}

// lastStaticDimension returns the last axis when it is a positive constant.
func lastStaticDimension(shape []int64) int {
	if len(shape) == 0 {
		return 0
	}
	value := shape[len(shape)-1]
	if value <= 0 {
		return 0
	}
	return int(value)
}

// secondStaticDimension returns the second axis when it is a positive constant.
func secondStaticDimension(shape []int64) int {
	if len(shape) < 2 {
		return 0
	}
	value := shape[1]
	if value <= 0 {
		return 0
	}
	return int(value)
}

// isWindows and isDarwin name the platform without importing four build-tagged files for two
// comparisons.
func isWindows() bool { return runtime.GOOS == "windows" }
func isDarwin() bool  { return runtime.GOOS == "darwin" }

// Describe names the input and output the session was built with, for a diagnostics panel.
func (m *modelInputs) Describe() string {
	if m == nil {
		return ""
	}
	return strings.Join([]string{"input_ids", "attention_mask", "token_type_ids"}, ", ")
}
