#pragma once

#include <cstddef>
#include <cstdint>
#include <filesystem>
#include <string>
#include <vector>

#include "BaseNavPack.h"
#include "RecastNavGridIO.h"

namespace navmesh::recast
{

// 预烘场旁包 (BNVF v2)。主包每条 span 记录在这里各有六列: 类内强连通分量号、重判禁步位、
// 立面段位、台阶税边位、封缝后净空、中轴位; 每类一张分量间的有向无环图; 每条边界边一张留它的类表。
// 运行期靠它把"可达域 / 禁步重判 / 挑墙 / 台阶税 / 封缝净空 / 中轴"全省掉, 无封堵的腿只剩解瓦与 A*。
inline constexpr uint32_t kFieldsRulesVersion = 2;
inline constexpr uint16_t kFieldsFormatVersion = 2;

// 主包路径 → 旁包路径: base.nav.gz → base.fields.nav.gz, 同一目录。
std::filesystem::path FieldsSidecarPath(const std::filesystem::path& main_pack);

struct FieldsTileRef
{
    uint32_t records = 0;
    const uint8_t* data = nullptr; // 载荷首字节, 指进 FieldsPack 自有的字节
    size_t len = 0;
};

struct FieldsZoneDir
{
    std::string name;
    uint32_t global_regions = 0;
    std::vector<FieldsTileRef> tiles; // 与 BGRD 同区的瓦表同序
    const uint8_t* scc = nullptr;
    size_t scc_len = 0;
    const uint8_t* wal = nullptr;
    size_t wal_len = 0;
};

// 一块瓦的六列, 与主包同一块瓦解出的记录逐条对位。
struct FieldsTile
{
    std::vector<uint32_t> scc;    // 类内分量号 1..n_scc; 填充记录为 0
    std::vector<uint8_t> steps2x; // 重判禁步位 XOR 主包 steps 位
    std::vector<uint8_t> seg;     // bit0 = c→b 出段, bit1 = b→c 出段; 只有正交两向有值
    std::vector<uint8_t> tax;     // 台阶税边位: 方向 i 的正向在 bit 2i, 反向在 bit 2i+1
    std::vector<uint16_t> clr2d;  // 主包 clr 减去封缝后净空 (≥ 0), 平方格数
    std::vector<uint8_t> medial;  // bit0 = 该格在类内的中轴上
};

// 一个区的整类量。
struct FieldsZone
{
    struct Region
    {
        uint32_t n_scc = 0;
        uint32_t adj_off = 0; // adj_start 里这一类的起点, 长 n_scc + 2 (分量号 1 起)
    };

    std::vector<Region> regions; // 按类号索引
    std::vector<uint32_t> adj_start;
    std::vector<uint32_t> edge_dst;
    std::vector<uint32_t> wall_key; // 升序: 区内三角号 * 3 + 边内序
    std::vector<uint32_t> wall_rid_start;
    std::vector<uint32_t> wall_rid;

    // 从分量 s0 出发能到的分量集, 下标是分量号(0 位不用)。类号越界或 s0 越界给空表。
    std::vector<uint8_t> reachFrom(uint32_t rid, uint32_t s0) const;

    // 边 (tri, k) 在类 rid 的层上是否留下。不在表里的边是数据不一致, known 置 false。
    bool wallKeep(int32_t tri, int k, uint32_t rid, bool& known) const;
};

// 离网连接 (FLNK 段) 的一头: 烘焙侧已对着 BGRD 挑好落脚格与 span。格号与 BGRD 瓦片同一全局格。
struct FieldsLinkSide
{
    int32_t gx = 0;
    int32_t gy = 0;
    float h = 0.0F;      // 选中 span 在 BGRD 里的高
    float decl_h = 0.0F; // 源声明的落脚高
    uint32_t rid = 0;
    uint16_t clr = 0;
    uint8_t flags = 0;
    uint8_t why = 0; // 0 可落脚; 1 出界; 2 无面; 3 高度带内无面; 4 带内只有 core==0 的面
};

// 一条离网连接, BOML 字段原样转述; valid bit0 = lo 可落脚, bit1 = hi 可落脚。
struct FieldsLinkRec
{
    uint32_t boml_index = 0;
    uint16_t zone_id = 0;
    uint8_t kind = 0;
    uint8_t valid = 0;
    int32_t is_ext = 0;
    int32_t bidirectional = 0;
    int32_t area = 0;
    uint16_t link_type = 0;
    uint8_t direction = 0;
    float radius = 0.0F;
    float cost_modifier = 0.0F;
    FieldsLinkSide lo;
    FieldsLinkSide hi;
};

struct FieldsLinkZone
{
    uint16_t zone_id = 0;
    uint32_t first = 0;
    uint32_t len = 0;
};

// 可打通的死格 (FOPN 段): 烘焙侧已按墙类过滤, 仅收录被缝类墙覆盖且未接触崖/实墙/虚空的死记录
// 以及源声明的离网落点。解瓦时匹配 (gx, gy, h 位模式) 即并入 flags, 净空取较大值。
struct FieldsOpenRec
{
    int32_t gx = 0;
    int32_t gy = 0;
    float h = 0.0F;
    uint16_t clr = 0; // 打通后的净空, 平方格数
    uint8_t flags = 0;
    uint8_t src = 0; // bit0 = 缝门控, bit1 = 离网落点
};

// 在区内按 (gy, gx, h 位模式) 升序的区段中查找一条; 未找到返回空。
const FieldsOpenRec* FindOpen(const FieldsOpenRec* p, size_t n, int32_t gx, int32_t gy, float h);

class FieldsPack
{
public:
    // 读旁包并对着主包与格图目录逐项核对: 身份、判据常数、区表、每瓦记录数。
    // 任一项不符即失败, 不做降级 —— 判据不一致的场比没有场更糟。
    bool load(const std::filesystem::path& path, const BaseNavPack& main, const GridPack& grid, std::string& err);

    bool valid() const { return loaded_; }

    const FieldsZoneDir* findZone(const std::string& name) const;

    bool decodeTile(const FieldsTileRef& t, FieldsTile& out) const;

    bool loadZone(const FieldsZoneDir& z, FieldsZone& out, std::string& err) const;

    // 区 zone_id 的离网连接, 按 BOML 下标升序; 旁包没有 FLNK 段或该区没有连接时 n = 0。
    const FieldsLinkRec* linksOfZone(uint16_t zone_id, size_t& n) const;

    const std::vector<FieldsLinkRec>& links() const { return links_; }

    // 区 zone_id 的可打通死格, 按 (gy, gx, h) 升序; 没有 FOPN 段或该区没有时 n = 0。
    const FieldsOpenRec* opensOfZone(uint16_t zone_id, size_t& n) const;

private:
    std::vector<uint8_t> bytes_;
    bool loaded_ = false;
    uint16_t flags_ = 0;
    std::vector<FieldsZoneDir> zones_;
    std::vector<FieldsLinkRec> links_;
    std::vector<FieldsLinkZone> link_zones_;
    std::vector<FieldsOpenRec> opens_;
    std::vector<FieldsLinkZone> open_zones_;
};

}
