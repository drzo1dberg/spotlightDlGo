# Spotlight Download in Go
Minimal CLI to fetch **Windows Spotlight** wallpapers (landscape only) via Microsoft’s v4 selection API.  
- Always download, skip existing files
- Landscape only (no portrait/phone)
- No single/URL/action mode
- Works on Linux, macOS, Windows (Go binary)

## Install
```bash
mkdir wallpaper
go build -o spotlightdl
./spotlightdl -outdir ./wallpaper -locale en-US -v
```


`LICENSE` (MIT):
```text
MIT License

Copyright (c) 2025 drzo1dberg 
Permission is hereby granted, free of charge, to any person obtaining a copy
