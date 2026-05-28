package tensor

import (
	"math"
	"testing"
)

func TestMatMul(t *testing.T) {
	a, err := FromData([]float32{1, 2, 3, 4, 5, 6}, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	b, err := FromData([]float32{7, 8, 9, 10, 11, 12}, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	got, err := MatMul(a, b)
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{58, 64, 139, 154}
	for i := range want {
		if got.Data[i] != want[i] {
			t.Fatalf("matmul[%d] = %v, want %v", i, got.Data[i], want[i])
		}
	}
}

func TestSoftmaxCrossEntropy(t *testing.T) {
	logits := []float32{1, 2, 3}
	probs, err := Softmax(logits)
	if err != nil {
		t.Fatal(err)
	}
	var sum float32
	for _, p := range probs {
		sum += p
	}
	if math.Abs(float64(sum-1)) > 1e-5 {
		t.Fatalf("softmax sum = %v", sum)
	}
	loss, grad, err := CrossEntropy(logits, 2)
	if err != nil {
		t.Fatal(err)
	}
	if loss <= 0 {
		t.Fatalf("loss = %v, want positive", loss)
	}
	var gradSum float32
	for _, g := range grad {
		gradSum += g
	}
	if math.Abs(float64(gradSum)) > 1e-5 {
		t.Fatalf("grad sum = %v, want 0", gradSum)
	}
}

func TestClipGradNormAndAdamW(t *testing.T) {
	params := [][]float32{{1, -2}}
	grads := [][]float32{{3, 4}}
	norm := ClipGradNorm(grads, 1)
	if math.Abs(norm-5) > 1e-6 {
		t.Fatalf("norm = %v, want 5", norm)
	}
	if math.Abs(float64(grads[0][0])-0.6) > 1e-5 || math.Abs(float64(grads[0][1])-0.8) > 1e-5 {
		t.Fatalf("clipped grads = %v", grads[0])
	}
	opt := NewAdamW(0.1, 0, params)
	before := append([]float32(nil), params[0]...)
	if err := opt.Update(params, grads); err != nil {
		t.Fatal(err)
	}
	if params[0][0] == before[0] && params[0][1] == before[1] {
		t.Fatal("adamw did not update params")
	}
}
