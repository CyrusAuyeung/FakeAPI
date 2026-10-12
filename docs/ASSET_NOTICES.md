# Asset provenance

`assets/manifest.yaml` is the source of truth for each asset's authorship,
origin and license. Exactly four ASCII character portraits are included.
They are text renderings of user-provided character references; the original
raster images are not included in the repository or release archives.

`LicenseRef-Character-Reference` identifies character-reference-derived material.
The MIT software license does not grant rights to the underlying character IP.
Other bundled text, SVG and JSON fixtures were authored for this project under
MIT. Imported assets retain the license entered by their author.

The four portraits can be replaced with other permitted text assets using
`fakeapi assets import`. Service configuration is a startup snapshot; restart
after changing the manifest or rules.
