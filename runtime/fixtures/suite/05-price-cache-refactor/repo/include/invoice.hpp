#pragma once
#include "catalog.hpp"
#include <utility>
#include <vector>
using Cart=std::vector<std::pair<std::string,int>>;
class InvoiceService {
    const Catalog& catalog_;
    std::uint64_t revision_=0;
    std::map<Cart,std::int64_t> cache_;
public:
    explicit InvoiceService(const Catalog& catalog):catalog_(catalog),revision_(catalog.revision()){}
    std::int64_t quote(const Cart& cart);
};
