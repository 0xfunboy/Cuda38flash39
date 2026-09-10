#pragma once
#include <cstdint>
#include <string>
#include <vector>
struct Request { int units; bool privileged; };
struct Policy { std::string route; std::int64_t (*evaluate)(Request); };
std::vector<Policy> registry();
