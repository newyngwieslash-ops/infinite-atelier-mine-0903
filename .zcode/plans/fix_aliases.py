import io

p = 'web/src/services/desktop/drama.ts'
s = io.open(p, encoding='utf-8').read()

old = '''export async function runStoryboardStage(
    request: desktop.RunScriptStageRequest,
): Promise<desktop.ScriptStageResultDTO> {
    // The production stages go through the SAME two commands the script ones do, because
    // they are the same mechanism: WP-09 extracted `stagepipeline` so one binding serves
    // both layers, and the layer the stage belongs to is what decides which pipeline runs.
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { RunScriptStage } = await loadDramaBinding();
    return RunScriptStage(request);
}

export async function runStoryboardSupervision(
    request: desktop.RunScriptSupervisionRequest,
): Promise<desktop.ReviewReportDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { RunScriptSupervision } = await loadDramaBinding();
    return RunScriptSupervision(request);
}'''

new = '''// The production stages go through the SAME binding the script ones do, and the claim that
// makes that true was verified rather than assumed: the binding holds BOTH pipelines in
// separate slots and routes on the stage a command names, so a production stage cannot reach
// the script layer — and if it did, the layer would refuse it with the stage named.
export const runStoryboardStage = runScriptStage;
export const runStoryboardSupervision = runScriptSupervision;'''

assert old in s, "alias anchor"
s = s.replace(old, new, 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("aliases written")
