import {buildRegions, rawJson} from "./model.mjs";

export default buildRegions().map((region) => ({
    Key: region.key,
    Name: region.name,
    Entry: region.entry,
    ActionParam: rawJson({path: region.path}),
}));
