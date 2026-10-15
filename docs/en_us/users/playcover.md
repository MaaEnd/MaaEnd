# macOS PlayCover

Only Apple Silicon Macs are supported. Intel Macs are not supported.

PlayCover support is making steady progress alongside ADB support. We have almost no macOS developers, so if you run into problems, please don't hesitate to report them in an Issue.

> [!IMPORTANT]
>
> In the following situations, macOS limitations prevent MaaEnd from capturing the game screen, which leads to errors.
>
> - Minimizing the game window
> - Moving the game window to another desktop or display
> - Making another window full screen
> - Switching to another window while Stage Manager is on
>
> Apart from these situations, screenshots work normally even when the game window is covered by other windows.

## Install and configure PlayCover and Arknights: Endfield

1. Download and install the [PlayCover fork that supports MaaTools](https://github.com/hguandl/PlayCover/releases).
2. Download a decrypted Arknights: Endfield IPA file from [decrypt.day](https://decrypt.day/app/id6753859465) or another source you trust, then drag it into the PlayCover main window to install it.
3. In PlayCover, change the settings of Arknights: Endfield.
    - Graphics:
        - iOS device: `iPad Pro (13-inch) (7th gen) | M4 | 8GB`
        - Resolution: `Custom`
            - Width: `1280`
            - Height: `720`
        - Resolution Scaler: `1.0`
        - Turn on `Disable display sleep`
    - Bypasses:
        - Turn on `Enable PlayChain (Experimental)`
        - Turn on `MaaTools`; you can set your own port number
        - Turn on `Capture from Metal`
        - Turn on `Enable Jailbreak Bypass (Experimental)`
        - Turn on `Insert Introspection libraries`
        - Turn on `Force Insert iOS Frameworks`
        - Turn on `Prevent usleep call spamming`
    - Misc:
        - Application Type: `public.app-category.games`
4. Double-click Arknights: Endfield in PlayCover to launch it. The game's title bar should read `明日方舟：终末地 [localhost:<port>]`.
5. Open the in-game settings and set the frame rate to `60 FPS`.

> [!NOTE]
>
> When Arknights: Endfield needs a client update, redo step 2 and check the settings in step 3.
>
> After PlayCover is updated, check the settings in step 3 and redo step 4. **You must launch the game once in PlayCover to complete the update.**

## Configure MaaEnd

In the Connection Settings at the top right of the MaaEnd window, select the `PlayCover` controller, and enter the text inside the square brackets of the game's title bar as the connection address, e.g. `localhost:<port>`.

### Launch the game automatically

Add the `Special Tasks` - `Pre-Program` task, and in `Program Path` enter the path of the game's Unix executable, not the .app path. For example, `/Users/<username>/Library/Containers/io.playcover.PlayCover/Applications/com.hypergryph.endfield.app/Endfield`.

### Close the game automatically

Add the `Special Tasks` - `Kill Process` task, turn off `Kill Self`, and in `Process Name` enter the game's process name `Endfield`.
