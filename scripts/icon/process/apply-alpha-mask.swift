#!/usr/bin/env swift

import CoreImage
import Foundation
import ImageIO

guard CommandLine.arguments.count == 4 else {
  fputs("Usage: apply-alpha-mask.swift <base.png> <mask.png> <output.png>\n", stderr)
  exit(1)
}

let baseURL = URL(fileURLWithPath: CommandLine.arguments[1])
let maskURL = URL(fileURLWithPath: CommandLine.arguments[2])
let outputURL = URL(fileURLWithPath: CommandLine.arguments[3])

func loadImage(_ url: URL) -> CIImage? {
  guard let source = CGImageSourceCreateWithURL(url as CFURL, nil),
        let image = CGImageSourceCreateImageAtIndex(source, 0, nil) else {
    return nil
  }
  return CIImage(cgImage: image)
}

guard let baseImage = loadImage(baseURL), let maskImage = loadImage(maskURL) else {
  fputs("Failed to load base or mask PNG.\n", stderr)
  exit(1)
}

guard baseImage.extent.size == maskImage.extent.size else {
  fputs("Base and mask PNGs must have the same dimensions.\n", stderr)
  exit(1)
}

guard let filter = CIFilter(name: "CIBlendWithAlphaMask") else {
  fputs("The CIBlendWithAlphaMask filter is unavailable.\n", stderr)
  exit(1)
}

filter.setValue(baseImage, forKey: kCIInputImageKey)
filter.setValue(CIImage(color: .clear).cropped(to: baseImage.extent), forKey: kCIInputBackgroundImageKey)
filter.setValue(maskImage, forKey: "inputMaskImage")

guard let outputImage = filter.outputImage else {
  fputs("Failed to apply the alpha mask.\n", stderr)
  exit(1)
}

let context = CIContext(options: [.workingColorSpace: NSNull()])
let colorSpace = CGColorSpaceCreateDeviceRGB()

try FileManager.default.createDirectory(
  at: outputURL.deletingLastPathComponent(),
  withIntermediateDirectories: true
)

try context.writePNGRepresentation(
  of: outputImage,
  to: outputURL,
  format: .RGBA8,
  colorSpace: colorSpace,
  options: [:]
)
