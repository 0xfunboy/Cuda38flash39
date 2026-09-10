#pragma once
#include <cstdint>
#include <utility>
#include <vector>
using Interval=std::pair<std::int64_t,std::int64_t>;
std::vector<Interval> available(Interval work,std::vector<Interval> blocked);
