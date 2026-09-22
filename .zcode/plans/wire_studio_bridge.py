import io

p = 'web/monoform-studio/src/App.jsx'
s = io.open(p, encoding='utf-8').read()

# 1. The import.
old_import = "import { MainViewport, CameraPreview } from './Viewport.jsx'"
new_import = """import { MainViewport, CameraPreview } from './Viewport.jsx'
import { cameraFromOverride, isEmbedded, sendExport, sendShotUpdated, subscribeToHost } from './bridge.js'"""
assert old_import in s, "import anchor"
s = s.replace(old_import, new_import, 1)

# 2. The application state: the nonce from the host and the shot the host opened.
old_state = """  const [settings, setSettings] = useState(() => normalizeProjectSettings(startupProject?.settings))"""
new_state = """  // The MONOFORM bridge: the nonce the host minted for this mount, and the shot it asked
  // this panel to open. Both are set by the host's own message — the studio never invents a
  // nonce, because a value the receiver made up is not a protection for the receiver.
  const [bridgeNonce, setBridgeNonce] = useState('')
  const [openedShot, setOpenedShot] = useState(null)
  const [settings, setSettings] = useState(() => normalizeProjectSettings(startupProject?.settings))"""
assert old_state in s, "settings anchor"
s = s.replace(old_state, new_state, 1)

# 3. The listener and the shot-apply effect, after the firstEffect that syncs currentFrame.
old_effect = """  useEffect(() => {
    currentFrameRef.current = currentFrame
  }, [currentFrame])"""
new_effect = """  useEffect(() => {
    currentFrameRef.current = currentFrame
  }, [currentFrame])

  // The host's messages: `open_shot` is what this studio does with them.
  //
  // `subscribeToHost` validates the envelope, the version and the origin before calling back,
  // so this handler only ever sees a message from the embedding host. When this studio is not
  // embedded the subscription is a no-op, which is what makes the standalone build work.
  useEffect(() => {
    if (!isEmbedded()) return undefined
    return subscribeToHost(({ nonce, shot }) => {
      setBridgeNonce(nonce)
      setOpenedShot(shot)
    })
  }, [])

  // The opened shot becomes a SHOT in this studio, named after the one the board holds.
  //
  // It is added rather than loaded over the user's work: a director panel opened from shot 6
  // must not silently discard the scene the user was already building. The camera is applied
  // when the plan recorded one, and left at the studio's default when it did not — a camera
  // nobody chose is worse than none.
  const appliedShotIdsRef = useRef(new Set())
  useEffect(() => {
    if (!openedShot || !openedShot.shotId) return
    if (appliedShotIdsRef.current.has(openedShot.shotId)) return
    appliedShotIdsRef.current.add(openedShot.shotId)
    const camera = cameraFromOverride(openedShot.overridesJson)
    const id = `shot-${openedShot.shotId}`
    setShots(prev => {
      if (prev.some(shot => shot.id === id)) return prev
      const label = openedShot.shotNumber ? `镜头 ${openedShot.shotNumber}` : `镜头 ${openedShot.shotId}`
      return [
        ...prev,
        {
          id,
          name: label,
          thumbnail: '',
          fps: DEFAULT_PROJECT_SETTINGS.fps,
          durationSeconds: openedShot.durationSeconds > 0 ? openedShot.durationSeconds : DEFAULT_PROJECT_SETTINGS.durationSeconds,
          loopPlayback: false,
          objects: cloneProjectValue(initialObjects),
          camera: camera ? { ...cloneProjectValue(initialCamera), ...camera } : cloneProjectValue(initialCamera),
          lighting: cloneProjectValue(DEFAULT_LIGHTING),
          reference: cloneProjectValue(DEFAULT_REFERENCE),
          keyframes: [],
          objectKeyframes: {},
        },
      ]
    })
    setActiveShotId(id)
    setToast(`已从分镜打开 · ${openedShot.shotNumber || openedShot.shotId}`)
    // The host's context is applied once per shot, and `appliedShotIdsRef` is what keeps a
    // re-render from adding it again.
  }, [openedShot])

  // The camera is reported to the host when the user saves, which is FR-060's "保存后可在
  // Shot 中看到摄像机参数". The report carries the ACTIVE shot when it came from the host, so a
  // camera edited on any other shot is not written to the board's row.
  const reportCameraToHost = useCallback(() => {
    if (!bridgeNonce || !openedShot?.shotId) return false
    return sendShotUpdated(bridgeNonce, openedShot.shotId, {
      position: [...camera.position],
      rotation: [...camera.rotation],
      focalLength: camera.focalLength,
      aspectRatio: camera.aspectRatio,
    })
  }, [bridgeNonce, openedShot, camera])"""
assert old_effect in s, "effect anchor"
s = s.replace(old_effect, new_effect, 1)

# 4. The two export postMessage calls become envelope messages to a named origin.
old_image_export = """      // Notify the embedding canvas host so the frame can be inserted directly as a canvas image node.
      if (window.parent && window.parent !== window) {
        window.parent.postMessage({ source: 'monoform', type: 'export', kind: 'image', blob, width, height }, '*')
      }"""
new_image_export = """      // Notify the embedding canvas host so the frame can be inserted directly as a canvas image node.
      //
      // The message goes through the bridge, which carries the envelope the host validates and
      // names the parent as its TARGET rather than posting to '*'. A message to '*' reaches
      // whatever document holds this frame, which is the defect SECURITY §12 names.
      const delivered = sendExport(bridgeNonce, 'image', blob)
      if (!delivered && isEmbedded()) {
        // Embedded but undeliverable: the host never sent an `open_shot`, so this studio has
        // no nonce and the host would refuse the message anyway. Saying so is better than a
        // silent download the user did not ask for.
        setToast('已导出到本地：本次预演未从分镜打开，因此没有卡' + '片接收这个画面')
      }"""
assert old_image_export in s, "image export anchor"
s = s.replace(old_image_export, new_image_export, 1)

old_video_export = """      // Notify the embedding canvas host so the video can be inserted directly as a canvas video node.
      if (window.parent && window.parent !== window) {
        window.parent.postMessage({ source: 'monoform', type: 'export', kind: 'video', blob }, '*')
      }"""
new_video_export = """      // Notify the embedding canvas host so the video can be inserted directly as a canvas video node,
      // through the same validated envelope the still uses.
      sendExport(bridgeNonce, 'video', blob)"""
assert old_video_export in s, "video export anchor"
s = s.replace(old_video_export, new_video_export, 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("studio wired to the bridge")
