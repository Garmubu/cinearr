# Changelog

All notable changes for users are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/) and versions follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

- The welcome screen of the first launch and the first page of the Windows installer carry the motto of Cinearr: *« Pour que la culture soit accessible au plus grand nombre »*.

## [0.4.2]

- mpv: the subtitle chosen in Cinearr is the one shown at the start, and every other subtitle (files and those inside the video) stays available during playback; *No subtitles* starts without any but keeps them all. VLC keeps the other subtitle files of the folder too.
- The player, the folders opened in the Explorer and the updater open in front of the other windows (Windows); on macOS the player is opened like an application, in front.
- Leaving the settings with changes not saved is refused: a red box offers to save or discard them, and shakes at each new attempt.
- Posters are those of the original version of each film or series, whatever the language of the interface; the library gets them at the first launch of this version.

## [0.4.1]

- The windows to choose a folder or a file open in front of Cinearr instead of behind it.
- OpenSubtitles.com is in the subtitle providers from the first launch, with the key provided with Cinearr; edit it to enter your own key or account, or remove it.
- Uninstall no longer talks about installed players: deleting the data removes the data folder of Cinearr only (with the players it downloaded there), never another program, film or download.
- README: a shorter presentation, without the file size.

## [0.4.0]

- Choose the subtitle (or none) active at the launch, next to the **Play** button of a film and in the details of an episode (VLC, mpv and MPC-HC).
- After adding a film or a series, you stay where you were to add other titles; the poster shows that it is in the library.
- The pages of films and series no longer jump back to the top every few seconds, and an opened subtitle list keeps its choice.
- Release languages: MULTi is found whatever surrounds it (`[MULTi]`, `MULTi-GROUP`, `(MULTI)`…), so such releases get their languages and their points; a release without any language tag is taken to be in the original language of the film or series instead of “Unknown”.
- The *Score* column of the manual search is now *Bonus*, with the detail of its points.
- The poster of a series with nothing downloaded no longer shows a badge.
- Uninstall: one window with **Uninstall** and **Cancel** and a box to delete the data too; from *Settings › Apps*, the buttons say **Keep the data** and **Delete the data**.

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
