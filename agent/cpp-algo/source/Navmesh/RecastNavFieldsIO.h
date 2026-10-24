#pragma once

#include <cstddef>
#include <cstdint>
#include <filesystem>
#include <memory>
#include <string>
#include <vector>

#include "BaseNavPack.h"
#include "RecastNavGridIO.h"

namespace navmesh::recast
{

// 旁包 BNVF, 主包每条 span 在此另有六列, 外加每类的分量图与每条边界边的留墙类表
inline constexpr uint32_t kFieldsRulesVersion = 2;
inline constexpr uint16_t kFieldsFormatVersion = 2;

// base.nav.gz → base.fields.nav.gz, 同一目录
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

// 与主包同一块瓦解出的记录逐条对位
struct FieldsTile
{
    std::vector<uint32_t> scc;    // 类内分量号 1..n_scc, 填充记录为 0
    std::vector<uint8_t> steps2x; // 重判禁步位 XOR 主包 steps 位
    std::vector<uint8_t> seg;     // bit0 = c→b 出段, bit1 = b→c 出段, 只有正交两向有值
    std::vector<uint8_t> tax;     // 台阶税边位: 方向 i 的正向在 bit 2i, 反向在 bit 2i+1
    std::vector<uint16_t> clr2d;  // 主包 clr 减去封缝后净空 (≥ 0), 平方格数
    std::vector<uint8_t> medial;  // bit0 = 该格在类内的中轴上
};

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

    // 下标是分量号, 0 位不用, rid 或 s0 越界时返回空表
    std::vector<uint8_t> reachFrom(uint32_t rid, uint32_t s0) const;

    // 不在表里的边属于数据不一致, known 置 false
    bool wallKeep(int32_t tri, int k, uint32_t rid, bool& known) const;
};

// 离网连接的一端, gx/gy 与 BGRD 瓦片同为全局格号
struct FieldsLinkSide
{
    int32_t gx = 0;
    int32_t gy = 0;
    float h = 0.0F;      // 选中 span 在 BGRD 里的高
    float decl_h = 0.0F; // 源声明的落脚高
    uint32_t rid = 0;
    uint16_t clr = 0;
    uint8_t flags = 0;
    uint8_t why = 0; // 0 可落脚, 1 出界, 2 无面, 3 高度带内无面, 4 带内只有 core==0 的面
};

// valid bit0 = lo 可落脚, bit1 = hi 可落脚
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

// 只往上坐, cost_px 是坐一次折成的走路 px, ride_s 是按下交互到人到顶的秒数
struct FieldsRopeRec
{
    uint32_t uid_bottom = 0;
    uint32_t uid_top = 0;
    FieldsLinkSide bottom;
    FieldsLinkSide top;
    float cost_px = 0.0F;
    float ride_s = 0.0F;
};

struct FieldsLinkZone
{
    uint16_t zone_id = 0;
    uint32_t first = 0;
    uint32_t len = 0;
};

// 解瓦时按 (gx, gy, h 位模式) 匹配并入 flags, 净空取较大值
struct FieldsOpenRec
{
    int32_t gx = 0;
    int32_t gy = 0;
    float h = 0.0F;
    uint16_t clr = 0; // 打通后的净空, 平方格数
    uint8_t flags = 0;
    uint8_t src = 0;  // bit0 = 缝门控, bit1 = 离网落点
};

// p 须按 (gy, gx, h 位模式) 升序
const FieldsOpenRec* FindOpen(const FieldsOpenRec* p, size_t n, int32_t gx, int32_t gy, float h);

class FieldsPack
{
public:
    bool load(const std::filesystem::path& path, const BaseNavPack& main, const GridPack& grid, std::string& err);

    bool valid() const { return loaded_; }

    const FieldsZoneDir* findZone(const std::string& name) const;

    bool decodeTile(const FieldsTileRef& t, FieldsTile& out) const;

    bool loadZone(const FieldsZoneDir& z, FieldsZone& out, std::string& err) const;

    // 按 BOML 下标升序, 没有时 n = 0
    const FieldsLinkRec* linksOfZone(uint16_t zone_id, size_t& n) const;

    const std::vector<FieldsLinkRec>& links() const { return links_; }

    // 按 (gy, gx, h) 升序, 没有时 n = 0
    const FieldsOpenRec* opensOfZone(uint16_t zone_id, size_t& n) const;

    const FieldsRopeRec* ropesOfZone(uint16_t zone_id, size_t& n) const;

private:
    std::vector<uint8_t> bytes_;
    bool loaded_ = false;
    uint16_t flags_ = 0;
    std::vector<FieldsZoneDir> zones_;
    std::vector<FieldsLinkRec> links_;
    std::vector<FieldsLinkZone> link_zones_;
    std::vector<FieldsOpenRec> opens_;
    std::vector<FieldsLinkZone> open_zones_;
    std::vector<FieldsRopeRec> ropes_;
    std::vector<FieldsLinkZone> rope_zones_;
};

bool LoadSharedFieldsPack(
    const std::filesystem::path& path,
    const BaseNavPack& main,
    const GridPack& grid,
    std::shared_ptr<const FieldsPack>& out,
    std::string& err);

}
