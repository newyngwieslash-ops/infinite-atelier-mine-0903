package memory

import (
	"encoding/binary"
	"math"
)

// Vector encoding, because PRD FR-120 is explicit about the storage form:
// "向量使用 FLOAT32/BLOB 或向量索引，不使用 JSON 数组长期存储".
//
// The format is the obvious one and is written down here rather than left to whichever
// code reads it: little-endian IEEE-754 binary32 values, four bytes each, no header. A
// header would carry the dimension and the model, and both are columns on the row
// already; repeating them in the blob would create a second place for them to disagree.
//
// It is in the DOMAIN package because it is a fact about what a vector IS in this system
// — the same bytes travel between the repository, the index and any future adapter — and
// because a pure encoder is a thing a test can pin exactly, which is the only way a byte
// layout survives a refactor.
const (
	// VectorBytesPerElement is the width of one element after encoding.
	VectorBytesPerElement = 4
	// MaxVectorDimensions bounds one vector, so a corrupt or hostile blob cannot make a
	// decode allocate without limit. It is far above any embedding model this build
	// knows of and exists as a ceiling rather than as a target.
	MaxVectorDimensions = 8192
	// MaxVectorBytes is the encoded size bound that follows from the two above.
	MaxVectorBytes = MaxVectorDimensions * VectorBytesPerElement
)

// EncodeVector renders a vector as the stored BLOB.
//
// A nil vector encodes to nil rather than to an empty slice, so "no embedding" and "an
// embedding of zero elements" stay distinguishable on the row.
func EncodeVector(vector []float32) []byte {
	if len(vector) == 0 {
		return nil
	}
	encoded := make([]byte, len(vector)*VectorBytesPerElement)
	for index, value := range vector {
		binary.LittleEndian.PutUint32(encoded[index*VectorBytesPerElement:], math.Float32bits(value))
	}
	return encoded
}

// DecodeVector reads a stored BLOB back.
//
// A length that is not a whole number of elements is a REFUSAL rather than a truncated
// read: a partial vector would compare against a query as though it were complete, which
// is the kind of wrong answer nothing downstream could detect.
func DecodeVector(encoded []byte) ([]float32, error) {
	if len(encoded) == 0 {
		return nil, nil
	}
	if len(encoded)%VectorBytesPerElement != 0 {
		return nil, InvalidError("A stored vector is not a whole number of float32 values.")
	}
	if len(encoded) > MaxVectorBytes {
		return nil, InvalidError("A stored vector is larger than any embedding this build supports.")
	}
	vector := make([]float32, len(encoded)/VectorBytesPerElement)
	for index := range vector {
		bits := binary.LittleEndian.Uint32(encoded[index*VectorBytesPerElement:])
		vector[index] = math.Float32frombits(bits)
	}
	return vector, nil
}

// Normalize returns the vector scaled to unit length.
//
// Section 12.3's MVP is "小规模归一化点积": when both sides are unit vectors the dot
// product IS the cosine similarity, which is why normalisation happens at WRITE time
// once per vector instead of at every comparison.
//
// A zero vector normalises to itself rather than dividing by zero. It then scores zero
// against everything, which is the honest answer for a vector with no direction.
func Normalize(vector []float32) []float32 {
	var sumSquares float64
	for _, value := range vector {
		sumSquares += float64(value) * float64(value)
	}
	if sumSquares == 0 {
		return vector
	}
	length := math.Sqrt(sumSquares)
	normalized := make([]float32, len(vector))
	for index, value := range vector {
		normalized[index] = float32(float64(value) / length)
	}
	return normalized
}

// Dot returns the dot product of two vectors.
//
// Vectors of different lengths score zero rather than panicking or comparing a prefix:
// two different embedding models produce different lengths, and a comparison between
// them is meaningless. Section 14.5 keeps them in separate index versions, so reaching
// here with a mismatch means something upstream was wrong, and zero is the value that
// cannot look like a good match.
func Dot(left, right []float32) float64 {
	if len(left) != len(right) || len(left) == 0 {
		return 0
	}
	var sum float64
	for index := range left {
		sum += float64(left[index]) * float64(right[index])
	}
	return sum
}
