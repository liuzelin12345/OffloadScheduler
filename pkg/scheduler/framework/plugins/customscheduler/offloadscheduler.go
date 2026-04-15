package customscheduler

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	resourcehelper "k8s.io/component-helpers/resource"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

// OffloadScheduler scores nodes by how closely their free resources match pod requests.
// It avoids always preferring "largest remaining capacity", which can increase fragmentation.
type OffloadScheduler struct{}

var _ framework.PreScorePlugin = &OffloadScheduler{}
var _ framework.ScorePlugin = &OffloadScheduler{}

// Name is the name of the plugin used in registry and scheduler configuration.
const (
	Name = "OffloadScheduler"

	preScoreStateKey = "PreScore" + Name
)

type preScoreState struct {
	podMilliCPU int64
	podMemory   int64
}

// Clone implements framework.StateData.
func (s *preScoreState) Clone() framework.StateData {
	return s
}

// Name returns the plugin name.
func (pl *OffloadScheduler) Name() string {
	return Name
}

// PreScore computes and stores the incoming pod's resource requests for scoring.
func (pl *OffloadScheduler) PreScore(ctx context.Context, cycleState *framework.CycleState, pod *v1.Pod, nodes []*framework.NodeInfo) *framework.Status {
	_ = ctx
	_ = nodes

	requests := resourcehelper.PodRequests(pod, resourcehelper.PodResourcesOptions{})
	state := &preScoreState{
		podMilliCPU: requests.Cpu().MilliValue(),
		podMemory:   requests.Memory().Value(),
	}
	cycleState.Write(preScoreStateKey, state)

	if state.podMilliCPU == 0 && state.podMemory == 0 {
		// BestEffort pods have no explicit requests; skip to avoid meaningless tie-breaking.
		return framework.NewStatus(framework.Skip)
	}
	return nil
}

func getPreScoreState(cycleState *framework.CycleState) (*preScoreState, error) {
	c, err := cycleState.Read(preScoreStateKey)
	if err != nil {
		return nil, fmt.Errorf("reading %q from cycleState: %w", preScoreStateKey, err)
	}

	s, ok := c.(*preScoreState)
	if !ok {
		return nil, fmt.Errorf("invalid PreScore state, got type %T", c)
	}
	return s, nil
}

// Score computes size-aware match score: nodes with the smallest post-fit leftover
// (but still fitting the pod) receive higher scores.
func (pl *OffloadScheduler) Score(ctx context.Context, cycleState *framework.CycleState, pod *v1.Pod, nodeInfo *framework.NodeInfo) (int64, *framework.Status) {
	_ = ctx

	state, err := getPreScoreState(cycleState)
	if err != nil {
		requests := resourcehelper.PodRequests(pod, resourcehelper.PodResourcesOptions{})
		state = &preScoreState{
			podMilliCPU: requests.Cpu().MilliValue(),
			podMemory:   requests.Memory().Value(),
		}
		if state.podMilliCPU == 0 && state.podMemory == 0 {
			return 0, nil
		}
	}

	return scoreNodeBySizeMatch(nodeInfo, state), nil
}

// ScoreExtensions returns nil because this plugin does not need NormalizeScore.
func (pl *OffloadScheduler) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

func scoreNodeBySizeMatch(nodeInfo *framework.NodeInfo, podReq *preScoreState) int64 {
	if nodeInfo == nil || podReq == nil {
		return 0
	}

	cpuScore := resourceMatchScore(nodeInfo.Allocatable.MilliCPU, nodeInfo.Requested.MilliCPU, podReq.podMilliCPU)
	memScore := resourceMatchScore(nodeInfo.Allocatable.Memory, nodeInfo.Requested.Memory, podReq.podMemory)

	var totalWeight float64
	var weightedScore float64
	if podReq.podMilliCPU > 0 {
		totalWeight += float64(podReq.podMilliCPU)
		weightedScore += cpuScore * float64(podReq.podMilliCPU)
	}
	if podReq.podMemory > 0 {
		totalWeight += float64(podReq.podMemory)
		weightedScore += memScore * float64(podReq.podMemory)
	}
	if totalWeight == 0 {
		return 0
	}

	score := int64(weightedScore / totalWeight)
	if score < framework.MinNodeScore {
		return framework.MinNodeScore
	}
	if score > framework.MaxNodeScore {
		return framework.MaxNodeScore
	}
	return score
}

func resourceMatchScore(allocatable, requested, podRequest int64) float64 {
	if podRequest == 0 {
		return float64(framework.MaxNodeScore)
	}
	if allocatable <= 0 {
		return 0
	}

	free := allocatable - requested
	if free < podRequest {
		return 0
	}

	leftover := free - podRequest
	score := (1 - float64(leftover)/float64(allocatable)) * float64(framework.MaxNodeScore)
	if score < float64(framework.MinNodeScore) {
		return float64(framework.MinNodeScore)
	}
	if score > float64(framework.MaxNodeScore) {
		return float64(framework.MaxNodeScore)
	}
	return score
}

// New initializes a new OffloadScheduler plugin.
func New(_ context.Context, _ runtime.Object, _ framework.Handle) (framework.Plugin, error) {
	return &OffloadScheduler{}, nil
}
