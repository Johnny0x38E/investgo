# InvestGo icon resources

- `appicon.png` is the canonical 1024×1024 RGBA source used by the macOS and Windows build scripts. Its transparent corners are intentional.
- `source/` keeps the generated artwork and the transparent cleanup intermediate for future design revisions.
- `process/render-app-icon.sh` copies or renders the canonical source into `build/appicon.png` and synchronizes the About-page asset at `frontend/src/assets/app-mark.png`.
- `process/make-rounded-mask.swift` creates a deterministic rounded-corner alpha mask for icon silhouette adjustments.
- `process/apply-alpha-mask.swift` applies a reviewed mask without redrawing or recoloring the interior artwork.
- `process/render-svg-icon.swift` is the optional SVG fallback used when `SOURCE_FILE` points to an SVG.

The current Wails/legacy `.icns` packaging path preserves the source PNG alpha instead of applying a Dock mask, so the master itself must contain the rounded corners. Keep the artwork centered, keep the outer corners transparent, and use the generous rounded-square radius in the current canonical source. The packaging script then creates the standard 1x/2x `iconutil` representations for the `.icns` file.

If the app later moves to native layered Icon Composer assets, revisit Apple’s square-layer guidance and let that newer asset pipeline own the mask.

Reference: <https://developer.apple.com/design/human-interface-guidelines/app-icons>
