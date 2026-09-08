"""Minimal Kitty peer used to verify ma's RGBA transfer and cached crops."""
import base64
import struct
import zlib


def png(width, height, pixels):
    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))
    rows = b"".join(b"\x00" + pixels[y*width*4:(y+1)*width*4] for y in range(height))
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(rows)) + chunk(b"IEND", b""))


class KittyPeer:
    def __init__(self):
        self.images = {}
        self.upload = None
        self.payload = bytearray()
        self.uploads = 0
        self.placements = 0

    def command(self, header, data):
        fields = dict(part.split("=", 1) for part in header.decode().split(",") if "=" in part)
        action = fields.get("a")
        if action == "q":
            return "query", None
        if action == "t":
            assert fields.get("f") == "32" and fields.get("o") == "z", "pager must use compressed RGBA"
            self.upload = fields
            self.payload.clear()
        if action == "t" or (action is None and "m" in fields):
            assert len(data) <= 4096, "oversized graphics chunk"
            self.payload.extend(data)
            if fields.get("m") == "0":
                image_id, width, height = (int(self.upload[key]) for key in ("i", "s", "v"))
                pixels = zlib.decompress(base64.b64decode(self.payload))
                assert len(pixels) == width*height*4, "wrong RGBA pixel count"
                self.images[image_id] = (width, height, pixels)
                self.uploads += 1
                self.upload = None
                self.payload.clear()
        elif action == "p":
            width, height, pixels = self.images[int(fields["i"])]
            x, y, w, h = (int(fields.get(key, 0)) for key in ("x", "y", "w", "h"))
            assert 0 <= x < width and 0 <= y < height and x+w <= width and y+h <= height, "crop outside cached image"
            cropped = b"".join(pixels[((y+row)*width+x)*4:((y+row)*width+x+w)*4] for row in range(h))
            self.placements += 1
            return "frame", (w, h, cropped)
        elif action == "d" and fields.get("d") == "I":
            self.images.pop(int(fields["i"]), None)
        return None, None
