#pragma once
#include <map>
#include <string>
using Inventory=std::map<std::string,int>;
void import_inventory(Inventory& inventory,const std::string& csv);
