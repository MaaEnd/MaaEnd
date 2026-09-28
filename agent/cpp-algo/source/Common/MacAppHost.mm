#include "MacAppHost.h"

#include <atomic>

#import <Cocoa/Cocoa.h>

@interface MaaEndAppDelegate : NSObject <NSApplicationDelegate>
@end

@implementation MaaEndAppDelegate

- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication*)sender
{
    NSAppleEventDescriptor* event = [[NSAppleEventManager sharedAppleEventManager] currentAppleEvent];
    if ([event attributeDescriptorForKeyword:kAEQuitReason] != nil) {
        return NSTerminateNow;
    }
    for (NSWindow* window in sender.windows) {
        [window performClose:nil];
    }
    return NSTerminateCancel;
}

@end

namespace common::macapp
{

namespace
{

std::atomic_bool g_running { false };
MaaEndAppDelegate* g_app_delegate = nil;

NSMenuItem* AddItem(NSMenu* menu, NSString* title, SEL action, NSString* key)
{
    return [menu addItemWithTitle:title action:action keyEquivalent:key];
}

void InstallMainMenu()
{
    NSMenu* main_menu = [[NSMenu alloc] init];

    NSMenuItem* app_item = [[NSMenuItem alloc] init];
    [main_menu addItem:app_item];
    app_item.submenu = [[NSMenu alloc] init];

    NSMenuItem* edit_item = [[NSMenuItem alloc] init];
    [main_menu addItem:edit_item];
    NSMenu* edit_menu = [[NSMenu alloc] initWithTitle:@"Edit"];
    edit_item.submenu = edit_menu;
    AddItem(edit_menu, @"Undo", @selector(undo:), @"z");
    NSMenuItem* redo = AddItem(edit_menu, @"Redo", @selector(redo:), @"z");
    redo.keyEquivalentModifierMask = NSEventModifierFlagCommand | NSEventModifierFlagShift;
    [edit_menu addItem:[NSMenuItem separatorItem]];
    AddItem(edit_menu, @"Cut", @selector(cut:), @"x");
    AddItem(edit_menu, @"Copy", @selector(copy:), @"c");
    AddItem(edit_menu, @"Paste", @selector(paste:), @"v");
    AddItem(edit_menu, @"Select All", @selector(selectAll:), @"a");

    NSMenuItem* window_item = [[NSMenuItem alloc] init];
    [main_menu addItem:window_item];
    NSMenu* window_menu = [[NSMenu alloc] initWithTitle:@"Window"];
    window_item.submenu = window_menu;
    AddItem(window_menu, @"Close", @selector(performClose:), @"w");

    NSApp.mainMenu = main_menu;
}

} // namespace

void RunMainLoop()
{
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
        g_app_delegate = [[MaaEndAppDelegate alloc] init];
        NSApp.delegate = g_app_delegate;
        InstallMainMenu();
        g_running = true;
        [NSApp run];
        g_running = false;
    }
}

void QuitMainLoop()
{
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp stop:nil];
        NSEvent* wake = [NSEvent otherEventWithType:NSEventTypeApplicationDefined
                                           location:NSZeroPoint
                                      modifierFlags:0
                                          timestamp:0
                                       windowNumber:0
                                            context:nil
                                            subtype:0
                                              data1:0
                                              data2:0];
        [NSApp postEvent:wake atStart:YES];
    });
}

bool IsMainLoopRunning()
{
    return g_running;
}

} // namespace common::macapp
