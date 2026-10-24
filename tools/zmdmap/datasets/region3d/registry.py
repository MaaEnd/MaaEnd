"""从当版数据里发现出来的地区输入；这里不存地区白名单。"""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class EntryAnimationBinding:
    asset: str
    root_path: str = ""


@dataclass(frozen=True, slots=True)
class RegionRegistration:
    region_id: str
    name_id: str
    region_prefab: str
    mount_keys: tuple[str, ...]
    entry_animations: tuple[EntryAnimationBinding, ...]
    ui_paths: dict[str, str] = field(default_factory=dict[str, str])


@dataclass(frozen=True, slots=True)
class RegionRegistry:
    regions: tuple[RegionRegistration, ...]
    camera_path: str
    ui_node_path: str
    panel_path: str
    panel_entry_animation: str

    def all_resources(self) -> list[str]:
        paths = {self.camera_path, self.ui_node_path, self.panel_path, self.panel_entry_animation}
        for region in self.regions:
            paths.add(region.region_prefab)
            paths.update(binding.asset for binding in region.entry_animations)
            paths.update(region.ui_paths.values())
        return sorted(paths)
