# Credits

Cinearr is a derivative work, distributed under GPL-3.0, of free software projects whose logic, regular expressions and data it reuses. Thanks to their authors and contributors.

| Project | License | Reused in Cinearr |
|---|---|---|
| [Radarr](https://github.com/Radarr/Radarr) | GPL-3.0 | `internal/parser` (Parser, QualityParser, LanguageParser, ReleaseGroupParser), `internal/quality` (Quality, QualityDefinition, QualityFinder, MediaFileExtensions, FileExtensions), `internal/decision` (specifications and DownloadDecisionComparer), `internal/qbit` (QBittorrentProxyV2), `internal/dlclient` (TransmissionProxy and TransmissionBase, DelugeProxy and Deluge, UTorrentProxy and UTorrent), `internal/app/importer.go` (DownloadedMovieImportService: import and safety checks), `internal/app/monitor.go` (download client polling, failed download handling), parser test cases in `internal/parser/testdata` |
| [Sonarr](https://github.com/Sonarr/Sonarr) | GPL-3.0 | Series support: naming schemes of `internal/parser/episode.go` (ParsedEpisodeInfo), episode decisions and season pack / episode count ranking in `internal/decision/episodes.go` (DownloadDecisionComparer), season then episode searches in `internal/app/series.go` (ReleaseSearchService), import layout |
| [Prowlarr](https://github.com/Prowlarr/Prowlarr) | GPL-3.0 | `internal/indexer/cardigann` (CardigannBase, CardigannParser, CardigannRequestGenerator, CardigannDefinition), Newznab categories, MagnetLinkBuilder, ParseUtil, DateTimeUtil, template tests |
| [Prowlarr Indexers](https://github.com/Prowlarr/Indexers) | see the repository | YAML site definitions, downloaded at runtime from `indexers.prowlarr.com` (not included in the repository) |
| [Cardigann](https://github.com/cardigann/cardigann) | see the repository | Original format of the indexer definitions |
| [Bazarr](https://github.com/morpheus65535/bazarr) | GPL-3.0 | `internal/subtitles`: film and episode subtitle scores (subliminal_patch/score.py), logic of the OpenSubtitles.com, SubDL, Subsource, YIFY Subtitles, Subsynchro, Gestdown (Addic7ed), TVsubtitles and BetaSeries providers |
| [subliminal](https://github.com/Diaoul/subliminal) | MIT | Equivalent release groups, OpenSubtitles hash, TVsubtitles download pages |
| [Gestdown](https://www.gestdown.info) | — | Public API of the Addic7ed subtitles, used by the Gestdown provider (no code reused) |
| [Jellyfin](https://github.com/jellyfin/jellyfin) | GPL-2.0 | Inspiration only, no code reused: playback is handed to a native player started as an external program |

Supported players (not included in the executable; VLC and MPC-HC can be downloaded by Cinearr from their official sites, at the user's request): [VLC](https://www.videolan.org/vlc/) (GPL-2.0, VideoLAN), [MPC-HC](https://github.com/clsid2/mpc-hc) (GPL-3.0), [mpv](https://mpv.io) (GPL-2.0+ / LGPL).

The .NET-compatible regular expression engine (`internal/rx`), the HTML/CSS selector engine (`internal/dom`, replacing AngleSharp) and the YAML reader (`internal/yaml`) were written for Cinearr so that the expressions and definitions above run unchanged.

Film and series data: [TMDB](https://www.themoviedb.org). This product uses the TMDB API but is not endorsed or certified by TMDB.
