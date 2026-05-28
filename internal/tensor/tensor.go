package tensor

import (
	"fmt"
	"math"
)

type Dense struct {
	Shape []int
	Data  []float32
}

func New(shape ...int) Dense {
	size := 1
	for _, dim := range shape {
		if dim <= 0 {
			panic("tensor dimension must be positive")
		}
		size *= dim
	}
	return Dense{Shape: append([]int(nil), shape...), Data: make([]float32, size)}
}

func FromData(data []float32, shape ...int) (Dense, error) {
	size := 1
	for _, dim := range shape {
		if dim <= 0 {
			return Dense{}, fmt.Errorf("tensor dimension must be positive")
		}
		size *= dim
	}
	if size != len(data) {
		return Dense{}, fmt.Errorf("shape size %d does not match data length %d", size, len(data))
	}
	return Dense{Shape: append([]int(nil), shape...), Data: append([]float32(nil), data...)}, nil
}

func MatMul(a, b Dense) (Dense, error) {
	if len(a.Shape) != 2 || len(b.Shape) != 2 {
		return Dense{}, fmt.Errorf("matmul expects rank-2 tensors")
	}
	m, k := a.Shape[0], a.Shape[1]
	if b.Shape[0] != k {
		return Dense{}, fmt.Errorf("matmul shape mismatch: %v x %v", a.Shape, b.Shape)
	}
	n := b.Shape[1]
	out := New(m, n)
	for i := 0; i < m; i++ {
		for j := 0; j < n; j++ {
			var sum float32
			for p := 0; p < k; p++ {
				sum += a.Data[i*k+p] * b.Data[p*n+j]
			}
			out.Data[i*n+j] = sum
		}
	}
	return out, nil
}

func EmbeddingLookup(table []float32, vocabSize, dim int, ids []int) (Dense, error) {
	if len(table) != vocabSize*dim {
		return Dense{}, fmt.Errorf("embedding table shape mismatch")
	}
	out := New(len(ids), dim)
	for i, id := range ids {
		if id < 0 || id >= vocabSize {
			return Dense{}, fmt.Errorf("token id %d outside vocab size %d", id, vocabSize)
		}
		copy(out.Data[i*dim:(i+1)*dim], table[id*dim:(id+1)*dim])
	}
	return out, nil
}

func RMSNorm(x []float32, eps float32) []float32 {
	var meanSq float32
	for _, v := range x {
		meanSq += v * v
	}
	meanSq /= float32(len(x))
	scale := float32(1 / math.Sqrt(float64(meanSq+eps)))
	out := make([]float32, len(x))
	for i, v := range x {
		out[i] = v * scale
	}
	return out
}

func Softmax(logits []float32) ([]float32, error) {
	if len(logits) == 0 {
		return nil, fmt.Errorf("softmax needs at least one logit")
	}
	maxLogit := logits[0]
	for _, v := range logits {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, fmt.Errorf("invalid logit %v", v)
		}
		if v > maxLogit {
			maxLogit = v
		}
	}
	out := make([]float32, len(logits))
	var sum float64
	for i, v := range logits {
		e := math.Exp(float64(v - maxLogit))
		out[i] = float32(e)
		sum += e
	}
	if sum == 0 || math.IsNaN(sum) || math.IsInf(sum, 0) {
		return nil, fmt.Errorf("invalid softmax denominator")
	}
	for i := range out {
		out[i] /= float32(sum)
	}
	return out, nil
}

func CrossEntropy(logits []float32, target int) (float64, []float32, error) {
	if target < 0 || target >= len(logits) {
		return 0, nil, fmt.Errorf("target %d outside logits length %d", target, len(logits))
	}
	probs, err := Softmax(logits)
	if err != nil {
		return 0, nil, err
	}
	p := float64(probs[target])
	if p <= 0 {
		return 0, nil, fmt.Errorf("target probability is zero")
	}
	grad := append([]float32(nil), probs...)
	grad[target] -= 1
	return -math.Log(p), grad, nil
}

func CausalMean(table []float32, vocabSize, dim int, tokens []int, pos int) ([]float32, error) {
	if pos < 0 || pos >= len(tokens) {
		return nil, fmt.Errorf("position %d outside token length %d", pos, len(tokens))
	}
	if len(table) != vocabSize*dim {
		return nil, fmt.Errorf("embedding table shape mismatch")
	}
	out := make([]float32, dim)
	for i := 0; i <= pos; i++ {
		id := tokens[i]
		if id < 0 || id >= vocabSize {
			return nil, fmt.Errorf("token id %d outside vocab size %d", id, vocabSize)
		}
		row := table[id*dim : (id+1)*dim]
		for d := 0; d < dim; d++ {
			out[d] += row[d]
		}
	}
	scale := float32(1.0 / float64(pos+1))
	for d := range out {
		out[d] *= scale
	}
	return out, nil
}

func ApplyRoPE(q, k []float32, position int) {
	dim := len(q)
	if len(k) < dim {
		dim = len(k)
	}
	dim -= dim % 2
	for i := 0; i < dim; i += 2 {
		theta := float64(position) / math.Pow(10000, float64(i)/float64(dim))
		c, s := float32(math.Cos(theta)), float32(math.Sin(theta))
		q0, q1 := q[i], q[i+1]
		k0, k1 := k[i], k[i+1]
		q[i], q[i+1] = q0*c-q1*s, q0*s+q1*c
		k[i], k[i+1] = k0*c-k1*s, k0*s+k1*c
	}
}

func SwiGLU(a, b []float32) ([]float32, error) {
	if len(a) != len(b) {
		return nil, fmt.Errorf("swiglu length mismatch")
	}
	out := make([]float32, len(a))
	for i := range a {
		silu := a[i] / (1 + float32(math.Exp(float64(-a[i]))))
		out[i] = silu * b[i]
	}
	return out, nil
}

func ClipGradNorm(grads [][]float32, maxNorm float64) float64 {
	var sumSq float64
	for _, grad := range grads {
		for _, v := range grad {
			sumSq += float64(v) * float64(v)
		}
	}
	norm := math.Sqrt(sumSq)
	if maxNorm <= 0 || norm <= maxNorm || norm == 0 {
		return norm
	}
	scale := float32(maxNorm / norm)
	for _, grad := range grads {
		for i := range grad {
			grad[i] *= scale
		}
	}
	return norm
}

type AdamW struct {
	LearningRate float64
	Beta1        float64
	Beta2        float64
	Eps          float64
	WeightDecay  float64
	Step         int
	M            [][]float32
	V            [][]float32
}

func NewAdamW(lr, weightDecay float64, params [][]float32) *AdamW {
	m := make([][]float32, len(params))
	v := make([][]float32, len(params))
	for i, p := range params {
		m[i] = make([]float32, len(p))
		v[i] = make([]float32, len(p))
	}
	return &AdamW{
		LearningRate: lr,
		Beta1:        0.9,
		Beta2:        0.999,
		Eps:          1e-8,
		WeightDecay:  weightDecay,
		M:            m,
		V:            v,
	}
}

func (a *AdamW) Update(params [][]float32, grads [][]float32) error {
	if len(params) != len(grads) || len(params) != len(a.M) {
		return fmt.Errorf("adamw param/grad length mismatch")
	}
	a.Step++
	b1, b2 := a.Beta1, a.Beta2
	lr := a.LearningRate
	for pi := range params {
		if len(params[pi]) != len(grads[pi]) || len(params[pi]) != len(a.M[pi]) {
			return fmt.Errorf("adamw tensor %d length mismatch", pi)
		}
		for i := range params[pi] {
			g := float64(grads[pi][i])
			a.M[pi][i] = float32(b1*float64(a.M[pi][i]) + (1-b1)*g)
			a.V[pi][i] = float32(b2*float64(a.V[pi][i]) + (1-b2)*g*g)
			mhat := float64(a.M[pi][i]) / (1 - math.Pow(b1, float64(a.Step)))
			vhat := float64(a.V[pi][i]) / (1 - math.Pow(b2, float64(a.Step)))
			decay := a.WeightDecay * float64(params[pi][i])
			params[pi][i] -= float32(lr * (mhat/(math.Sqrt(vhat)+a.Eps) + decay))
		}
	}
	return nil
}
