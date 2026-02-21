#include "my_reco_1.h"

#include <iostream>

#include <meojson/json.hpp>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

MaaBool ChildCustomRecognitionCallback(MaaContext* context, MaaTaskId task_id,
	const char* node_name,
	const char* custom_recognition_name,
	const char* custom_recognition_param,
	const MaaImageBuffer* image,
	const MaaRect* roi, void* trans_arg,
	/* out */ MaaRect* out_box,
	/* out */ MaaStringBuffer* out_detail) {
	LogInfo << VAR(context) << VAR(task_id) << VAR(node_name)
		<< VAR(custom_recognition_name) << VAR(custom_recognition_param)
		<< VAR(image) << VAR(roi) << VAR(trans_arg);

	auto* tasker = MaaContextGetTasker(context);
	std::ignore = tasker;

	if (out_box) {
		*out_box = { 100, 100, 10, 10 };
	}
	if (out_detail) {
		json::value j;
		j["key"] = "value";

		MaaStringBufferSet(out_detail, j.dumps().c_str());
	}

	return true;
}
