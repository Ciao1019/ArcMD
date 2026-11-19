# Original BotAvatar fixtures

Generated offline from the pinned upstream TypeScript, not from the native Go
implementation. See `third_party/bot-avatars/README.md` for source, license,
defaults and verification instructions.

`motion.json`: two seeds, each with 30 seconds at 60 Hz, sampled every six
frames. Inputs include pointer movement, release, a click, and transitions
through default → working → sleeping → default. `poses.json` selects eight
frames rendered by the original Canvas engine at size 32, DPR 2, on white.
The 96×96 images include the upstream 1.5× overscan region.
