import io

p = 'web/src/services/__tests__/monoform-bridge.spec.ts'
s = io.open(p, encoding='utf-8').read()

old_camera = '''    const camera = {
        positionX: 1,
        positionY: 2,
        positionZ: 3,
        targetX: 0,
        targetY: 1,
        targetZ: 0,
        fieldOfView: 50,
    };'''
new_camera = '''    // The camera is the STUDIO's own shape rather than one invented for the bridge: the
    // bridge carries a camera the studio produced and hands it back.
    const camera = {
        position: [1, 2, 3],
        rotation: [0, 45, 12],
        focalLength: 42,
        aspectRatio: "16:9",
    };'''
assert old_camera in s, "camera anchor"
s = s.replace(old_camera, new_camera)

s = s.replace('''    assert.equal(good.message.shotId, "shot-1");
    assert.equal(good.message.camera.fieldOfView, 50);
''', '''    assert.equal(good.message.shotId, "shot-1");
    assert.equal(good.message.camera.focalLength, 42);
    assert.deepEqual(good.message.camera.position, [1, 2, 3]);
''')

s = s.replace('''    // A camera with a missing component.
    const missing = { ...camera } as Record<string, unknown>;
    delete missing.positionZ;
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: missing })).ok, false);
    // A component that is not a finite number — NaN survives JSON and would poison the
    // stored camera, so it is refused rather than written.
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, targetX: Number.NaN } })).ok, false);
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, targetX: "1" } })).ok, false);
    // A field of view nothing can be rendered from. It is REFUSED rather than clamped,
    // because clamping would silently change the shot the user composed.
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, fieldOfView: 0 } })).ok, false);
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, fieldOfView: 180 } })).ok, false);''',
'''    // A camera whose position is not three components — a vector of two would produce a
    // camera the studio could not build, and the stored shot would be unopenable.
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, position: [1, 2] } })).ok, false);
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, rotation: null } })).ok, false);
    // A component that is not a finite number — NaN survives JSON and would poison the
    // stored camera, so it is refused rather than written.
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, position: [1, Number.NaN, 3] } })).ok, false);
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, position: ["1", 2, 3] } })).ok, false);
    // A focal length nothing can be rendered from. It is REFUSED rather than clamped,
    // because clamping would silently change the shot the user composed.
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, focalLength: 0 } })).ok, false);
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, focalLength: 5000 } })).ok, false);
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, focalLength: "42" } })).ok, false);
    // An aspect ratio is required and bounded: an empty one names no frame shape.
    assert.equal(validate(validMessage({ type: "shot_updated", shotId: "shot-1", camera: { ...camera, aspectRatio: "  " } })).ok, false);''')

s = s.replace('''    const camera = {
        positionX: 0, positionY: 0, positionZ: 0, targetX: 0, targetY: 0, targetZ: 0, fieldOfView: 40,
    };''','''    const camera = {
        position: [0, 1.6, 4] as [number, number, number],
        rotation: [0, 0, 0] as [number, number, number],
        focalLength: 35,
        aspectRatio: "16:9",
    };''')

io.open(p, 'w', encoding='utf-8').write(s)
print("spec updated to the studio's camera shape")
