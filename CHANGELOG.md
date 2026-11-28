# Changelog

All notable changes for users are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/) and versions follow [Semantic Versioning](https://semver.org/).

## [0.3.2]

- The setup wizard no longer asks for a TMDB key: the one provided with Cinearr is used. A key of your own can still be entered in *Settings › General*.
- *Reset Cinearr* also empties the lists of films and series of the library (files on disk are never touched), so films of the old folders no longer stay after a reset.
- At the end of the setup wizard, the films and series folders are read at once; the library no longer shows the report of a previous folder.
- The details of the library report (errors, missing files) no longer close by themselves, and the counts of the *Films* and *Series* tabs follow the library.

## [0.3.1]

- No key to create any more: the official releases include a TMDB key, so the film information works right away (the OpenSubtitles.com key was already included). Your own keys can still be entered and are used first.
- If TMDB or OpenSubtitles.com stops accepting a key provided with Cinearr, a message explains it and leads to the settings to enter your own.
- Changing the settings of a subtitle provider (a new key, an account) lifts its pause after repeated failures.
- Torrent client: switching to another client no longer erases the settings of the previous one; coming back to it brings back its address and account.
- Player: the field of *Other player* is no longer filled in with a player found or installed by Cinearr; the player you chose there is kept when you try another one.

## [0.2.0]

- Windows installer: choice of the folder, Start Menu and desktop shortcuts, `update.exe` to update and `uninstall.exe` to uninstall (with or without the data).
- macOS application (`.dmg`) and Linux install, update and uninstall scripts.
- Cinearr has its own icon.
- *Check for updates* in *Settings › Data and diagnostics*.
- When the language changes, the film and series pages of the library are reloaded in the new language.

## [0.1.0]

First public release.
