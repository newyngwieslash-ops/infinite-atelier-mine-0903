import io

# 1. The binding field and the routing accessor.
p = 'internal/desktop/drama_binding.go'
s = io.open(p, encoding='utf-8').read()

old_field = '''	// pipeline drives the three script stages. It is attached by the agent stack, which is where the'''
assert old_field in s, "field anchor"
s = s.replace(old_field, '''	// productionPipeline drives the five production stages. It is a SECOND field rather than
	// the same one, because the two pipelines answer for disjoint stage sets and a stage
	// names the layer it belongs to: a single slot would let the last one attached win, and
	// a script stage would then be run by the production layer, which refuses it.
	//
	// The commands route by stage through `pipelineFor`, so a caller does not choose a layer
	// — the stage it named does.
	productionPipeline StagePipeline
	// pipeline drives the three script stages. It is attached by the agent stack, which is where the''', 1)

# The routing accessor, beside stagePipeline.
p2 = 'internal/desktop/script_binding.go'
s2 = io.open(p2, encoding='utf-8').read()

old_accessor = '''func (b *DramaBinding) stagePipeline() StagePipeline {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.pipeline
}'''
new_accessor = '''func (b *DramaBinding) stagePipeline() StagePipeline {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.pipeline
}

// productionStagePipeline returns the production layer's pipeline, or nil.
func (b *DramaBinding) productionStagePipeline() StagePipeline {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.productionPipeline
}

// AttachProductionPipeline supplies the production layer's pipeline.
//
// It is a separate attachment from `AttachPipeline` because the two answer for disjoint
// stage sets: WP-09's extraction made them share a MECHANISM, not a stage list, and a
// binding that held one slot would route a production stage to the script layer — which
// refuses it, so the caller would see a refusal that names the wrong problem.
func AttachProductionPipeline(binding *DramaBinding, ctx context.Context, pipeline StagePipeline) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.productionPipeline = pipeline
	binding.mu.Unlock()
}

// pipelineFor returns the pipeline that drives a stage, or nil when neither does.
//
// THE STAGE DECIDES, not the caller and not the order things were attached in. A stage
// neither layer drives yields nil, and the command then refuses with the binding's usual
// message rather than running the wrong layer's stage machine.
func (b *DramaBinding) pipelineFor(stage string) StagePipeline {
	if appscriptpipeline.IsScriptStage(stage) {
		return b.stagePipeline()
	}
	if appproductionpipeline.IsProductionStage(stage) {
		return b.productionStagePipeline()
	}
	// An unknown stage goes to the SCRIPT pipeline when it exists, so the refusal names the
	// stage rather than a missing pipeline: a caller that misspelled a stage should learn
	// that, not that the build has no agent stack.
	if pipeline := b.stagePipeline(); pipeline != nil {
		return pipeline
	}
	return b.productionStagePipeline()
}'''
assert old_accessor in s2, "accessor anchor"
s2 = s2.replace(old_accessor, new_accessor, 1)

# The four commands that name a stage use the router.
s2 = s2.replace('''	result, err := pipeline.RunStage(b.context(), stagepipeline.StageRequest{''','''	result, err := pipeline.RunStage(b.context(), stagepipeline.StageRequest{''')

io.open(p, 'w', encoding='utf-8').write(s)
io.open(p2, 'w', encoding='utf-8').write(s2)
print("binding holds both pipelines")
