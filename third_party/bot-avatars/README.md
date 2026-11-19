# Bot Avatars: native ghost

Source: [Jakubantalik/Libraries.dev, bot-avatars](https://github.com/Jakubantalik/Libraries.dev/tree/387b049ca8fe97b5d72999b5bf61a19ab1ccff81/packages/bot-avatars),
commit `387b049ca8fe97b5d72999b5bf61a19ab1ccff81`, MIT © 2026 Jakub Antalik.
The requested component is `<BotAvatar type="ghost" size={32} />`: the
`default` state, eyes only, the stock white fabric material, default lights,
depth, fur, speed and jump settings. No new runtime dependencies.

`internal/app/qabotmotion.go` ports `src/engine.ts` with the stock settings:
head/eye pointer following (three head widths), independent gaze and eye
darts, blinking/double blinks, breathing, idle flips, click jumps with
crouch/landing squash, working hops/laughter, sleeping nods, and smooth
state changes. `qabot.go` binds native pointer/click/keyboard input and the
paint clock. Offscreen drawings stop requesting frames; reduced motion
uses `restPose` and does not jump or follow the pointer. The Q&A uses the
requested default state. The port does not expose the other avatar types,
accessories or customization props as an application settings UI.

`qabotpaint.go` renders the eyes from `src/draw.ts`, including the sphere
projection, curve quantization, lids and turned outline clip. The original
fabric shader is baked into view-dependent material images by the upstream
Canvas renderer: yaw 0–355° and pitch −45–30°, both every 5°, at 2× resolution.
These are body views, **not prerecorded motion**. The current live pose
interpolates four views, then draws its eyes and applies continuous squash,
roll and position. Each material sheet has 72 80×80 tiles (18 columns),
cropped at (8,8) from the original 96×96 overscan canvas. Two sheets stay in
memory; only the small composed bitmap is uploaded each frame. The ghost's
palette is intrinsic to the original asset; eye ink is in `theme.go`.

The motion is checked against 600 poses from the original engine, covering
two 30-second input/state sequences (`1e-6` tolerance). Eight original Canvas
images independently check the rendered rest, pointer, jump and state poses.
Material interpolation and native antialiasing mean pixels are not identical
to a browser render; this is a native visual port, not a React runtime.

## Verification

The material sprite sheets in `internal/app/assets/ghost` and the independent
reference fixtures in `internal/app/testdata/qabot` were captured offline from
the pinned upstream TypeScript Canvas renderer at commit
`387b049ca8fe97b5d72999b5bf61a19ab1ccff81` (`1152` material views, `600` motion
poses, and `8` reference PNGs).

Run `CGO_ENABLED=0 go test ./internal/app -run 'TestGhost|TestBotMotion'` to
verify the native port against those fixtures. Set `ARCMD_SNAPSHOT` to compare
the resulting native PNGs.
