#!/usr/bin/env swift

import AppKit
import Foundation

guard CommandLine.arguments.count == 8 else {
  fputs("Usage: make-rounded-mask.swift <output.png> <canvas-size> <x> <y> <width> <height> <radius>\n", stderr)
  exit(1)
}

let outputURL = URL(fileURLWithPath: CommandLine.arguments[1])
let values = CommandLine.arguments.dropFirst(2).compactMap(Double.init)

guard values.count == 6,
      values.allSatisfy({ $0 > 0 }) else {
  fputs("Canvas, bounds, and radius must be positive numbers.\n", stderr)
  exit(1)
}

let canvasSize = NSSize(width: values[0], height: values[0])
let pixelsWide = Int(values[0].rounded())
let pixelsHigh = Int(values[0].rounded())
let iconRect = NSRect(
  x: values[1],
  y: values[2],
  width: values[3],
  height: values[4]
)
let radius = min(values[5], min(iconRect.width, iconRect.height) / 2)

guard let bitmap = NSBitmapImageRep(
  bitmapDataPlanes: nil,
  pixelsWide: pixelsWide,
  pixelsHigh: pixelsHigh,
  bitsPerSample: 8,
  samplesPerPixel: 4,
  hasAlpha: true,
  isPlanar: false,
  colorSpaceName: .deviceRGB,
  bytesPerRow: 0,
  bitsPerPixel: 0
) else {
  fputs("Failed to allocate mask bitmap.\n", stderr)
  exit(1)
}

bitmap.size = canvasSize

NSGraphicsContext.saveGraphicsState()
guard let context = NSGraphicsContext(bitmapImageRep: bitmap) else {
  fputs("Failed to create mask graphics context.\n", stderr)
  exit(1)
}

NSGraphicsContext.current = context
context.imageInterpolation = .high
NSColor.clear.set()
NSBezierPath(rect: NSRect(origin: .zero, size: canvasSize)).fill()
NSColor.white.set()
NSBezierPath(roundedRect: iconRect, xRadius: radius, yRadius: radius).fill()
NSGraphicsContext.restoreGraphicsState()

guard let pngData = bitmap.representation(using: .png, properties: [:]) else {
  fputs("Failed to encode mask PNG.\n", stderr)
  exit(1)
}

try FileManager.default.createDirectory(
  at: outputURL.deletingLastPathComponent(),
  withIntermediateDirectories: true
)
try pngData.write(to: outputURL)
