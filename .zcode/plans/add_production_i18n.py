import io

KEYS_EN = '''            // WP-09: the two production sections. Each key names what the control does
            // rather than what it is, because the UI is the only place a user meets these
            // commands and the words have to carry the pipeline's own meaning.
            director: {
                selectEpisode: "Select an episode to see its director plan.",
                planVersions: "Plan versions",
                noPlan: "No director plan yet. Run the stage above to write one.",
                approve: "Approve this plan",
                approved: "Approved",
                approvedHint: "The plan in force. The storyboard stage is built from it.",
                openStudio: "Open the previs studio",
                studioHint: "Compose the shot in the 3D studio and save; the camera comes back to this row.",
                field: {
                    visualRhythm: "Visual rhythm",
                    cameraLanguage: "Camera language",
                    colorLighting: "Colour and lighting",
                    staging: "Staging",
                    continuityRules: "Continuity rules",
                    audioDirection: "Audio direction",
                },
            },
            storyboardTable: {
                selectEpisode: "Select an episode to see its storyboard.",
                noBoard: "No storyboard version yet. Run the storyboard stage above to write one.",
                versions: "Board versions",
                approve: "Approve this board",
                approved: "Approved",
                approvedHint: "The board in force. The image batch runs against it.",
                rows: "{{count}} rows",
                shot: "Shot",
                size: "Size",
                movement: "Movement",
                seconds: "Sec",
                visual: "Frame",
                action: "Action",
                audio: "Dialogue",
                continuity: "Continuity",
                frames: "First / last frame",
                motion: "Motion",
                panels: "Panels",
                edit: "Edit row",
                editHint: "Only this row is written. The other shots are untouched.",
                save: "Save row",
                conflict: "This row changed in another window. Reload and try again.",
                firstFrame: "First frame",
                lastFrame: "Last frame",
                videoMotion: "Video motion",
                duration: "Duration (seconds)",
                shotSize: "Shot size",
                cameraAngle: "Camera angle",
                cameraMovement: "Camera movement",
                dialogue: "Dialogue / narration",
            },
'''

KEYS_ZH = '''            // WP-09：两个生产分区。每个键说清楚控件做什么，而不是它是什么——UI 是用户唯一
            // 接触这些命令的地方，措辞必须承载流水线自己的含义。
            director: {
                selectEpisode: "选择一个剧集以查看其导演规划。",
                planVersions: "规划版本",
                noPlan: "尚无导演规划。运行上方阶段以生成一个。",
                approve: "批准此规划",
                approved: "已批准",
                approvedHint: "当前生效的规划，分镜阶段由它构建。",
                openStudio: "打开预演工作室",
                studioHint: "在 3D 工作室中构图并保存，摄像机参数会回写到本行。",
                field: {
                    visualRhythm: "视觉节奏",
                    cameraLanguage: "摄影语言",
                    colorLighting: "光线与色彩",
                    staging: "场面调度",
                    continuityRules: "连续性规则",
                    audioDirection: "声音方向",
                },
            },
            storyboardTable: {
                selectEpisode: "选择一个剧集以查看其分镜表。",
                noBoard: "尚无分镜版本。运行上方阶段以生成一个。",
                versions: "分镜版本",
                approve: "批准此分镜",
                approved: "已批准",
                approvedHint: "当前生效的分镜，批量生成图片针对它运行。",
                rows: "{{count}} 行",
                shot: "镜头",
                size: "景别",
                movement: "运动",
                seconds: "秒",
                visual: "画面",
                action: "动作",
                audio: "对白",
                continuity: "连续性",
                frames: "首帧 / 尾帧",
                motion: "运动描述",
                panels: "面板",
                edit: "编辑行",
                editHint: "只写入本行。其他镜头不受影响。",
                save: "保存本行",
                conflict: "本行已在另一个窗口中被修改。请重新加载后重试。",
                firstFrame: "首帧",
                lastFrame: "尾帧",
                videoMotion: "视频运动",
                duration: "时长（秒）",
                shotSize: "景别",
                cameraAngle: "机位",
                cameraMovement: "镜头运动",
                dialogue: "对白 / 旁白",
            },
'''

for path, block in (("web/src/i18n/locales/en-US.ts", KEYS_EN), ("web/src/i18n/locales/zh-CN.ts", KEYS_ZH)):
    s = io.open(path, encoding='utf-8').read()
    anchor = "    locale: {"
    assert anchor in s, "locale anchor in " + path
    s = s.replace(anchor, block + anchor, 1)
    io.open(path, 'w', encoding='utf-8').write(s)
    print("added to " + path)
