# NavMesh text file format: read / save
#
# File layout:
#   [MapTrackerNavMesh.Meta]
#   Version=1
#   Encoding=UTF-8
#   Name=...
#   Description=...
#   MapRegionName=...
#   MapLevelName=...
#   GeoWidth=...
#   GeoHeight=...
#
#   [MapTrackerNavMesh.Vertices]
#   V1=X...,Y...,T...,E...,F(...)
#
#   [MapTrackerNavMesh.Edges]
#   E1=S...,D...,B...,C...,F(...)

import math
import re
from dataclasses import dataclass

MAGIC_HEADER = "MapTrackerNavMesh"
VERSION = 1
ENCODING = "UTF-8"

FLAG_VERTEX_TELEPORT = 1
FLAG_VERTEX_HIDDEN = 2
FLAG_VERTEX_SYSTEM = 4
FLAG_VERTEX_RARE = 8
FLAG_VERTEX_COLLECTABLE = 16
FLAG_VERTEX_DIG = 32
FLAG_EDGE_BIDIRECTIONAL = 1

SECTION_META = f"{MAGIC_HEADER}.Meta"
SECTION_VERTICES = f"{MAGIC_HEADER}.Vertices"
SECTION_EDGES = f"{MAGIC_HEADER}.Edges"

_SECTION_RE = re.compile(r"^\s*\[(?P<section>[^\]]+)\]\s*$")
_KEY_VALUE_RE = re.compile(
    r"^\s*(?P<key>[A-Za-z][A-Za-z0-9_]*)\s*=\s*(?P<value>.*?)\s*$"
)
_FLOAT_RE = r"[-+]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?"
_POS_INT_RE = r"[1-9]\d*"
_INT_RE = r"[-+]?\d+"
_VERTEX_RE = re.compile(
    rf"^\s*V(?P<id>{_POS_INT_RE})\s*=\s*X(?P<x>{_FLOAT_RE})\s*,\s*Y(?P<y>{_FLOAT_RE})\s*,\s*T(?P<t>{_INT_RE})\s*,\s*E(?P<e>{_INT_RE})\s*,\s*F\((?P<flags>[A-Za-z]*)\)\s*$"
)
_EDGE_RE = re.compile(
    rf"^\s*E(?P<id>{_POS_INT_RE})\s*=\s*S(?P<from_id>{_POS_INT_RE})\s*,\s*D(?P<to_id>{_POS_INT_RE})\s*,\s*B(?P<bidirectional>[01])\s*,\s*C(?P<cost>{_FLOAT_RE})\s*,\s*F\((?P<flags>[A-Za-z]*)\)\s*$"
)


@dataclass
class NavVertex:
    id: int
    flags: int
    x: float
    y: float
    entity_id: int = 0
    tier_id: int = 0


@dataclass
class NavEdge:
    id: int
    flags: int
    from_id: int
    to_id: int
    cost: float = 0.0


@dataclass(frozen=True)
class NavMeshDataSnapshot:
    vertices: tuple[NavVertex, ...]
    edges: tuple[NavEdge, ...]
    next_vid: int
    next_eid: int


class NavMeshData:
    """Mutable navmesh data used by editors and tools."""

    def __init__(self) -> None:
        self.vertices: list[NavVertex] = []
        self.edges: list[NavEdge] = []
        self._next_vid = 1
        self._next_eid = 1

    # ── Create ──────────────────────────────────────────────────────────

    def new_vertex(
        self,
        x: float,
        y: float,
        flags: int = 0,
        entity_id: int = 0,
        tier_id: int = 0,
    ) -> NavVertex:
        v = NavVertex(
            id=self._next_vid,
            flags=flags,
            x=round(x, 3),
            y=round(y, 3),
            entity_id=entity_id,
            tier_id=tier_id,
        )
        self._next_vid += 1
        self.vertices.append(v)
        return v

    def new_edge(
        self,
        from_id: int,
        to_id: int,
        flags: int = FLAG_EDGE_BIDIRECTIONAL,
    ) -> NavEdge:
        e = NavEdge(id=self._next_eid, flags=flags, from_id=from_id, to_id=to_id)
        self._next_eid += 1
        self.edges.append(e)
        return e

    # ── Delete ──────────────────────────────────────────────────────────

    def delete_edge(self, edge_id: int) -> bool:
        for i, edge in enumerate(self.edges):
            if edge.id == edge_id:
                del self.edges[i]
                return True
        return False

    # ── Lookup by ID ────────────────────────────────────────────────────

    def get_vertex(self, vertex_id: int) -> NavVertex | None:
        for vertex in self.vertices:
            if vertex.id == vertex_id:
                return vertex
        return None

    def get_edge(self, edge_id: int) -> NavEdge | None:
        for edge in self.edges:
            if edge.id == edge_id:
                return edge
        return None

    # ── Graph queries ───────────────────────────────────────────────────

    def edges_for(self, vertex_id: int) -> list[NavEdge]:
        """Return all edges incident to *vertex_id*."""
        return [
            e
            for e in self.edges
            if e.from_id == vertex_id or e.to_id == vertex_id
        ]

    def neighbors(
        self, vertex_id: int, *, exclude_id: int | None = None
    ) -> list[NavVertex]:
        """Return neighboring vertices of *vertex_id*, optionally excluding one."""
        result: list[NavVertex] = []
        for edge in self.edges:
            nid: int | None = None
            if edge.from_id == vertex_id:
                nid = edge.to_id
            elif edge.to_id == vertex_id:
                nid = edge.from_id
            if nid is None or nid == exclude_id:
                continue
            v = self.get_vertex(nid)
            if v is not None:
                result.append(v)
        return result

    def has_edge_between(self, a_id: int, b_id: int) -> bool:
        for edge in self.edges:
            if (edge.from_id == a_id and edge.to_id == b_id) or (
                edge.from_id == b_id and edge.to_id == a_id
            ):
                return True
        return False

    # ── Spatial queries ─────────────────────────────────────────────────

    def get_nearest_vertex_for(
        self,
        x: float,
        y: float,
        *,
        max_distance: float,
        tier_id: int | None = None,
        exclude_ids: set[int] | None = None,
    ) -> NavVertex | None:
        nearest: NavVertex | None = None
        nearest_dist = max_distance
        for vertex in self.vertices:
            if tier_id is not None and vertex.tier_id != tier_id:
                continue
            if exclude_ids and vertex.id in exclude_ids:
                continue
            dist = math.hypot(vertex.x - x, vertex.y - y)
            if dist <= nearest_dist:
                nearest = vertex
                nearest_dist = dist
        return nearest

    # ── Snapshot / Restore ──────────────────────────────────────────────

    def snapshot(self) -> NavMeshDataSnapshot:
        return NavMeshDataSnapshot(
            vertices=tuple(
                NavVertex(v.id, v.flags, v.x, v.y, v.entity_id, v.tier_id) for v in self.vertices
            ),
            edges=tuple(
                NavEdge(e.id, e.flags, e.from_id, e.to_id, e.cost) for e in self.edges
            ),
            next_vid=self._next_vid,
            next_eid=self._next_eid,
        )

    def restore(self, snapshot: NavMeshDataSnapshot) -> None:
        self.vertices = [
            NavVertex(v.id, v.flags, v.x, v.y, v.entity_id, v.tier_id) for v in snapshot.vertices
        ]
        self.edges = [
            NavEdge(e.id, e.flags, e.from_id, e.to_id, e.cost) for e in snapshot.edges
        ]
        self._next_vid = snapshot.next_vid
        self._next_eid = snapshot.next_eid

    @classmethod
    def from_navmesh_file(cls, nmf: "NavMeshFile") -> "NavMeshData":
        data = cls()
        data.vertices = [
            NavVertex(v.id, v.flags, v.x, v.y, v.entity_id, v.tier_id) for v in nmf.vertices
        ]
        data.edges = [
            NavEdge(e.id, e.flags, e.from_id, e.to_id, 0.0) for e in nmf.edges
        ]
        data._next_vid = max((v.id for v in data.vertices), default=0) + 1
        data._next_eid = max((e.id for e in data.edges), default=0) + 1
        return data


class NavMeshFile:
    """NavMesh text reader/writer for `*.mtnm` files."""

    def __init__(
        self,
        name: str = "",
        description: str = "",
        map_region_name: str = "",
        map_level_name: str = "",
        geo_width: float = 0.0,
        geo_height: float = 0.0,
        vertices: list[NavVertex] | None = None,
        edges: list[NavEdge] | None = None,
        encoding: str = ENCODING,
    ) -> None:
        self.name = name
        self.description = description
        self.map_region_name = map_region_name
        self.map_level_name = map_level_name
        self.geo_width = float(geo_width)
        self.geo_height = float(geo_height)
        self.encoding = encoding
        self.vertices: list[NavVertex] = vertices if vertices is not None else []
        self.edges: list[NavEdge] = edges if edges is not None else []

    @staticmethod
    def _vertex_flags_to_text(flags: int) -> str:
        parts: list[str] = []
        if flags & FLAG_VERTEX_TELEPORT:
            parts.append("T")
        if flags & FLAG_VERTEX_HIDDEN:
            parts.append("H")
        if flags & FLAG_VERTEX_SYSTEM:
            parts.append("S")
        if flags & FLAG_VERTEX_RARE:
            parts.append("R")
        if flags & FLAG_VERTEX_COLLECTABLE:
            parts.append("C")
        if flags & FLAG_VERTEX_DIG:
            parts.append("D")
        return "".join(parts)

    @staticmethod
    def _vertex_flags_from_text(flag_text: str) -> int:
        flags = 0
        for ch in flag_text:
            if ch == "T":
                flags |= FLAG_VERTEX_TELEPORT
            elif ch == "H":
                flags |= FLAG_VERTEX_HIDDEN
            elif ch == "S":
                flags |= FLAG_VERTEX_SYSTEM
            elif ch == "R":
                flags |= FLAG_VERTEX_RARE
            elif ch == "C":
                flags |= FLAG_VERTEX_COLLECTABLE
            elif ch == "D":
                flags |= FLAG_VERTEX_DIG
            else:
                raise ValueError(f"Unsupported vertex flag: {ch!r}")
        return flags

    @staticmethod
    def _format_float(value: float) -> str:
        return repr(float(value))

    @staticmethod
    def _format_cost(value: float) -> str:
        text = f"{value:.3f}".rstrip("0").rstrip(".")
        return text if text else "0"

    @staticmethod
    def _parse_key_value(line: str) -> tuple[str, str]:
        match = _KEY_VALUE_RE.match(line)
        if not match:
            raise ValueError(f"Invalid key/value line: {line!r}")
        return match.group("key"), match.group("value")

    @staticmethod
    def _parse_section(line: str) -> str | None:
        match = _SECTION_RE.match(line)
        if not match:
            return None
        return match.group("section")

    def save(self, file_path: str) -> None:
        """Serialize this NavMesh to *file_path* in text `.mtnm` format."""
        lines: list[str] = [
            f"[{SECTION_META}]",
            f"Version={VERSION}",
            f"Encoding={ENCODING}",
            f"Name={self.name}",
            f"Description={self.description}",
            f"MapRegionName={self.map_region_name}",
            f"MapLevelName={self.map_level_name}",
            f"GeoWidth={self._format_float(self.geo_width)}",
            f"GeoHeight={self._format_float(self.geo_height)}",
            "",
            f"[{SECTION_VERTICES}]",
        ]

        for v in self.vertices:
            lines.append(
                "V{vid}=X{x},Y{y},T{tier},E{entity_id},F({flags})".format(
                    vid=v.id,
                    x=self._format_float(v.x),
                    y=self._format_float(v.y),
                    tier=int(v.tier_id),
                    entity_id=int(v.entity_id),
                    flags=self._vertex_flags_to_text(v.flags),
                )
            )

        vertex_lookup = {v.id: v for v in self.vertices}
        lines.extend(["", f"[{SECTION_EDGES}]"])
        for e in self.edges:
            src = vertex_lookup[e.from_id]
            dst = vertex_lookup[e.to_id]
            cost = math.hypot(src.x - dst.x, src.y - dst.y)
            lines.append(
                "E{eid}=S{from_id},D{to_id},B{bidirectional},C{cost},F()".format(
                    eid=e.id,
                    from_id=e.from_id,
                    to_id=e.to_id,
                    bidirectional=1 if (e.flags & FLAG_EDGE_BIDIRECTIONAL) else 0,
                    cost=self._format_cost(cost),
                )
            )

        lines.append("")

        with open(file_path, "w", encoding=ENCODING, newline="\n") as f:
            f.write("\n".join(lines))

    @staticmethod
    def read(file_path: str) -> "NavMeshFile":
        """Deserialize a `.mtnm` text file and return a :class:`NavMeshFile`."""
        with open(file_path, "r", encoding=ENCODING) as f:
            raw_lines = f.read().splitlines()

        current_section: str | None = None
        section_index = 0
        expected_sections = [SECTION_META, SECTION_VERTICES, SECTION_EDGES]
        meta: dict[str, str] = {}
        vertices: list[NavVertex] = []
        edges: list[NavEdge] = []
        seen_vertex_ids: set[int] = set()
        seen_edge_ids: set[int] = set()

        for raw_line in raw_lines:
            line = raw_line.strip()
            if line == "":
                continue

            section = NavMeshFile._parse_section(line)
            if section is not None:
                if (
                    section_index >= len(expected_sections)
                    or section != expected_sections[section_index]
                ):
                    raise ValueError(f"Unexpected NavMesh section: {section!r}")
                current_section = section
                section_index += 1
                continue

            if current_section is None:
                raise ValueError(f"Data found before first section: {raw_line!r}")

            if current_section == SECTION_META:
                key, value = NavMeshFile._parse_key_value(line)
                if key in meta:
                    raise ValueError(f"Duplicate Meta key: {key!r}")
                if key not in {
                    "Version",
                    "Encoding",
                    "Name",
                    "Description",
                    "MapRegionName",
                    "MapLevelName",
                    "GeoWidth",
                    "GeoHeight",
                }:
                    raise ValueError(f"Unexpected Meta key: {key!r}")
                meta[key] = value
                continue

            if current_section == SECTION_VERTICES:
                match = _VERTEX_RE.match(line)
                if not match:
                    raise ValueError(f"Invalid vertex line: {raw_line!r}")
                vid = int(match.group("id"))
                if vid in seen_vertex_ids:
                    raise ValueError(f"Duplicate vertex id: {vid}")
                seen_vertex_ids.add(vid)
                vertices.append(
                    NavVertex(
                        id=vid,
                        flags=NavMeshFile._vertex_flags_from_text(match.group("flags")),
                        x=round(float(match.group("x")), 3),
                        y=round(float(match.group("y")), 3),
                        entity_id=int(match.group("e")),
                        tier_id=int(match.group("t")),
                    )
                )
                continue

            if current_section == SECTION_EDGES:
                match = _EDGE_RE.match(line)
                if not match:
                    raise ValueError(f"Invalid edge line: {raw_line!r}")
                eid = int(match.group("id"))
                if eid in seen_edge_ids:
                    raise ValueError(f"Duplicate edge id: {eid}")
                seen_edge_ids.add(eid)
                flags = 0
                if match.group("bidirectional") == "1":
                    flags |= FLAG_EDGE_BIDIRECTIONAL
                if match.group("flags"):
                    raise ValueError(
                        f"Unsupported edge flag(s): {match.group('flags')!r}"
                    )
                edges.append(
                    NavEdge(
                        id=eid,
                        flags=flags,
                        from_id=int(match.group("from_id")),
                        to_id=int(match.group("to_id")),
                        cost=float(match.group("cost")),
                    )
                )
                continue

            raise ValueError(f"Unexpected section state: {current_section!r}")

        if section_index != len(expected_sections):
            raise ValueError("NavMesh file is missing required sections")

        required_meta_keys = {
            "Version",
            "Encoding",
            "Name",
            "Description",
            "MapRegionName",
            "MapLevelName",
            "GeoWidth",
            "GeoHeight",
        }
        missing_meta = required_meta_keys - meta.keys()
        if missing_meta:
            raise ValueError(f"NavMesh Meta is missing keys: {sorted(missing_meta)!r}")

        version = int(meta["Version"])
        if version != VERSION:
            raise ValueError(
                f"Unsupported NavMesh version: {version} (expected {VERSION})"
            )

        encoding = meta["Encoding"]
        if encoding != ENCODING:
            raise ValueError(f"Unsupported NavMesh encoding: {encoding!r}")

        name = meta["Name"]
        description = meta["Description"]
        map_region_name = meta["MapRegionName"]
        map_level_name = meta["MapLevelName"]
        geo_width = float(meta["GeoWidth"])
        geo_height = float(meta["GeoHeight"])

        if name == "":
            raise ValueError("NavMesh Name cannot be empty")
        if map_region_name == "":
            raise ValueError("NavMesh MapRegionName cannot be empty")
        if map_level_name == "":
            raise ValueError("NavMesh MapLevelName cannot be empty")
        if geo_width <= 0.0 or geo_height <= 0.0:
            raise ValueError("NavMesh GeoWidth and GeoHeight must be positive")

        vertex_lookup = {v.id: v for v in vertices}
        for edge in edges:
            if edge.from_id not in vertex_lookup:
                raise ValueError(
                    f"Edge {edge.id} references missing source vertex {edge.from_id}"
                )
            if edge.to_id not in vertex_lookup:
                raise ValueError(
                    f"Edge {edge.id} references missing target vertex {edge.to_id}"
                )

        return NavMeshFile(
            name=name,
            description=description,
            map_region_name=map_region_name,
            map_level_name=map_level_name,
            geo_width=geo_width,
            geo_height=geo_height,
            vertices=vertices,
            edges=edges,
            encoding=encoding,
        )
